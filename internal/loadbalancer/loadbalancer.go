package loadbalancer

import (
	"fmt"
	"net"
	crypto "networking/tcp/internal/cryptography"
	"networking/tcp/internal/logger"
	"networking/tcp/internal/protocol"
	"os"
	"strings"
	"sync"
	"time"
)

var log logger.Loggers = logger.NewLoggers(
	logger.WithConsole(os.Stdout, os.Stderr),
	logger.WithBaseOptions(
		logger.PrefixField("lbalancer"),
		logger.FormatField(logger.BASE_PREFIX),
	),
)

type LoadBalancer struct {
	msInfo map[protocol.ServiceType][]*protocol.MsInfo
	msConn map[string]*protocol.Connection

	apiConn map[string]*protocol.Connection

	listener net.Listener

	RWmu sync.RWMutex
}

func NewLoadBalancer(listPort string) (*LoadBalancer, error) {
	listen, err := net.Listen("tcp", listPort)
	if err != nil {
		return nil, err
	}

	return &LoadBalancer{
		listener: listen,
		msInfo:   make(map[protocol.ServiceType][]*protocol.MsInfo),
		msConn:   make(map[string]*protocol.Connection),
		apiConn:  make(map[string]*protocol.Connection),
	}, nil
}

func (lb *LoadBalancer) Start() {
	log["console"].Info("listening on %s", lb.listener.Addr().String())

	//not sure if this go func is needed because whole program is this loop
	//go func() {
		for {
			conn, err := lb.listener.Accept()
			if err != nil {
				log["console"].Error("Accept error: %w", err)
				continue
			}

			log["console"].Info("Accepted connection from %s", conn.RemoteAddr().String())
			go lb.handleConnection(conn)
		}
	//}()
}

func (lb *LoadBalancer) handleConnection(nc net.Conn) {
	conn := protocol.NewConnection(nc)

	lb.apiConn[conn.ID] = conn

	defer delete(lb.apiConn, conn.ID)
	defer conn.Close()

	for {
		request, err := protocol.Receive(conn.RW.Reader)
		if err != nil {
			log["console"].Error("Error while reading request: %v", err)
			return
		}

		//TODO? testing if putting this switch in go rutine will help with handling multiple clients properlly
		go func(request protocol.Message) {
			fmt.Println("Started gorutine on request", request)
			var response protocol.Message
			var respChan <-chan protocol.Message = nil
			
			switch request.Type {
			//TODO: later extend this to parsing if update should add, delete or update ms data  (add - add, del - delete, nothing - just update) as a 6th part in msg
			case protocol.UPDATE:
				//update microservice data
				ms, err := parseMsFromMessage(request)
				if err != nil {
					response = protocol.Message{
						ID:           request.ID,
						Type:         protocol.UPDATE,
						ConnectionID: request.ConnectionID,
						Code:         protocol.ERROR,
						Content:      err.Error(),
					}
					break
				}

				conn.RWmu.Lock()
				_, ok := lb.msInfo[ms.Type]
				conn.RWmu.Unlock()

				if !ok {
					
					if err = lb.connectToMicroservice(ms); err != nil {
						response = protocol.Message{
							ID:           request.ID,
							Type:         protocol.UPDATE,
							ConnectionID: request.ConnectionID,
							Code:         protocol.ERROR,
							Content:      err.Error(),
						}
						break
					}

					lb.msInfo[ms.Type] = append(lb.msInfo[ms.Type], ms)
					log["console"].Debug("Successfully added ms to register %v:%v type:%v\n", ms.Host, ms.Port, ms.Type)
				}
				
				response = protocol.Message{ID: request.ID, Type: request.Type, Code: protocol.SUCCESS}

			case protocol.HEARTBEAT:
				log["console"].Debug("Received heartbeat message")
				response = protocol.Message{ID: request.ID, Type: request.Type, Code: protocol.SUCCESS}

			//microservice operations
			case protocol.PING:
				fallthrough
			case protocol.IDLE:
				fallthrough
			case protocol.DOWNLOAD:
				fallthrough
			case protocol.UPLOAD:
				//forward message to microservice
				var msType protocol.ServiceType

				msType = protocol.ServiceType(request.Type)

				//take first ms with this type
				services := lb.msInfo[msType]

				if len(services) < 1 {
					log["console"].Error("no services with type %s available", msType)
					response = protocol.Message{
						ID:           request.ID,
						Type:         request.Type,
						ConnectionID: request.ConnectionID,
						Code:         protocol.ERROR,
						Content:      "no services available",
					}
					break
				}

				ms := services[0]

				msConn := lb.msConn[ms.ID]

				if msConn == nil {
					response = protocol.Message{
						ID:           request.ID,
						Type:         request.Type,
						Code:         protocol.ERROR,
						ConnectionID: request.ConnectionID,
						Content:      "Connection to ms is nil",
					}
					break
				}

				log["console"].Debug("Sending ms request: %v", request)
				respChan, err = msConn.SendRequestNew(request)
				//response, err = msConn.SendRequest(request)
				if err != nil {
					response = protocol.Message{
						ID:           request.ID,
						Type:         request.Type,
						Code:         protocol.ERROR,
						ConnectionID: request.ConnectionID,
						Content:      err.Error(),
					}
				}
				log["console"].Debug("Request was sent successfuly: %v", request)
			
			default:
				response = protocol.Message{
					ID: request.ID, Type: request.Type, Code: protocol.ERROR, Content: "unknown command: " + string(request.Type),
				}
			}

			if respChan == nil {
				response.ID = request.ID

				//log["console"].Debug("chan nil, response: %s", response)

				conn.RWmu.RLock()
				err := protocol.Send(conn.RW.Writer, response)
				conn.RWmu.RUnlock()
				
				if err != nil {
					log["console"].Error("Error sending response: %w", err)
					return
				}

			} else {

				log["console"].Debug("Waiting for channel %v messages...", respChan)
				for response := range respChan {
					response.ID = request.ID

					log["console"].Debug("chan %v got response: %v", respChan, response)

					conn.RWmu.RLock()
					err := protocol.Send(conn.RW.Writer, response)
					conn.RWmu.RUnlock()
					
					if err != nil {
						log["console"].Error("Error sending response: %w", err)
						return
					}
				}
			}
		}(request)

	}
}

// func (lb *LoadBalancer) handleIdle(msg protocol.Message, conn *protocol.Connection) {
// 	err := protocol.Send(conn.RW.Writer, msg)
// 	if err != nil {
// 		log["console"].Error("%v", err)
// 	}

// 	log["console"].Info("Sent Idle message")
// }

func parseMsFromMessage(msg protocol.Message) (*protocol.MsInfo, error) {
	if msg.Content == "" {
		err := logger.StrToError(log["string"], func() {
			log["string"].Error("Empty message while parsing ms")
		})
		return nil, err
	}

	log["console"].Debug("message:  %v\n", msg)

	dataSplit := strings.Split(msg.Content.(string), ";")

	expectedSplitCount := 5
	if len(dataSplit) != expectedSplitCount {
		err := logger.StrToError(log["string"], func() {
			log["string"].Error("Expected %d values while parsing ms, got: %v", expectedSplitCount, dataSplit)
		})
		return nil, err
	}

	log["console"].Debug("Created ms (%v %v %v %v %v)", dataSplit[0], dataSplit[1], dataSplit[2], dataSplit[3], dataSplit[4])

	return &protocol.MsInfo{
		ID:     dataSplit[0],
		Host:   dataSplit[1],
		Port:   dataSplit[2],
		NodeID: dataSplit[3],
		Type:   protocol.ServiceType(dataSplit[4]),
	}, nil
}

func (lb *LoadBalancer) connectToMicroservice(ms *protocol.MsInfo) error {
	address := net.JoinHostPort(ms.Host, ms.Port)

	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var nc net.Conn
	var err error

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout connecting to microservice %s", address)

		case <-ticker.C:
			nc, err = net.DialTimeout("tcp", address, 1*time.Second)
			if err == nil {
				conn := protocol.NewConnection(nc)
				conn.ID = crypto.GenerateID(crypto.CONN)

				lb.RWmu.Lock()
				lb.msConn[ms.ID] = conn
				lb.RWmu.Unlock()

				//go conn.ReceiveLoopNew(lb)
				go conn.ReceiveLoopNew(lb)

				log["console"].Info("Connected to microservice: %s", address)
				return nil
			}
		}
	}
}

// ################################# NODE METHODS #################################

func (lb *LoadBalancer) HandleHeartBeat(msg protocol.Message) {

}

func (lb *LoadBalancer) NodeAsyncEvent(request protocol.Message, conn *protocol.Connection) {

	switch request.Type {
	default:
		log["console"].Error("Unsupported NodeAsyncEvent type: %s", request.Type)
	}

}

// ################################# NODE METHODS #################################
