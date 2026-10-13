package loadbalancer

import (
	"fmt"
	"net"
	crypto "networking/tcp/internal/cryptography"
	json "networking/tcp/internal/json_helper"
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

	for {
		nc, err := lb.listener.Accept()
		if err != nil {
			log["console"].Error("Accept error: %w", err)
			continue
		}

		log["console"].Info("Accepted connection from %s", nc.RemoteAddr().String())
		go lb.handleConnection(nc)
	}
}

func (lb *LoadBalancer) handleConnection(nc net.Conn) {

	connID := fmt.Sprintf(
		"%s -> %s",
		nc.LocalAddr(),
		nc.RemoteAddr(),
	)

	log["console"].Debug(
		"[CONN %s] handleConnection START",
		connID,
	)

	requestPtr, conn := lb.registerConnection(nc)
	if requestPtr == nil || conn == nil {
		return
	}

	defer conn.Close()

	go lb.handleRequest(*requestPtr, conn)

	for {
		request, err := protocol.Receive(conn.RW.Reader)
		if err != nil {
			log["console"].Error("Handle connection: %v", err)
			return
		}

		go lb.handleRequest(request, conn)
	}
}

func (lb *LoadBalancer) registerConnection(nc net.Conn) (*protocol.Message, *protocol.Connection) {
	conn := protocol.NewConnection(nc)

	request, err := protocol.Receive(conn.RW.Reader)
	if err != nil {
		log["console"].Error("Register connection: %v", err)
		return nil, nil
	}

	if str, ok := request.Content.(string); ok && str == "CONTROLLER" {
		log["console"].Debug("Got controller connection!!! conn: %v", (conn.Nc.LocalAddr().String() + " -> " + conn.Nc.RemoteAddr().String()))

		go conn.ReceiveLoop(lb)

		go lb.SendHeartBeat(conn)
		return nil, nil
	}

	return &request, conn
}

func (lb *LoadBalancer) handleRequest(request protocol.Message, conn *protocol.Connection) {
	var response protocol.Message
	var respChan <-chan protocol.Message = nil
	var err error

	switch request.Type {
	case protocol.RECOVER:
		log["console"].Debug("Got recover message %v", request)
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
			msgStr := fmt.Sprintf("no services with type %s available", msType)
			log["console"].Error(msgStr)
			response = protocol.Message{
				ID:           request.ID,
				Type:         request.Type,
				ConnectionID: request.ConnectionID,
				Code:         protocol.ERROR,
				Content:      msgStr,
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
		respChan, err = msConn.SendRequestChan(request)
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
}

func (a *LoadBalancer) SendHeartBeat(conn *protocol.Connection) {

	msg := protocol.Message{
		Type:    protocol.HEARTBEAT,
		Content: "LB",
	}

	for {
		time.Sleep(5 * time.Second)

		msg.ID = crypto.GenerateID(crypto.MESSAGE_ID)
		if err := protocol.Send(conn.RW.Writer, msg); err != nil {
			log["console"].Error("Error sending heartbeat: %v", err)
		}
	}
}

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

	log["console"].Debug("Ms created from message (%v %v %v %v %v)", dataSplit[0], dataSplit[1], dataSplit[2], dataSplit[3], dataSplit[4])

	return &protocol.MsInfo{
		ID:     dataSplit[0],
		Host:   dataSplit[1],
		Port:   dataSplit[2],
		NodeID: dataSplit[3],
		Type:   protocol.ServiceType(dataSplit[4]),
	}, nil
}

func (lb *LoadBalancer) connectToMicroservice(ms *protocol.MsInfo) (*protocol.Connection, error) {
	address := net.JoinHostPort(ms.Host, ms.Port)

	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var nc net.Conn
	var err error

	for {
		select {
		case <-timeout:
			return nil, fmt.Errorf("timeout connecting to microservice %s", address)

		case <-ticker.C:
			nc, err = net.DialTimeout("tcp", address, 1*time.Second)
			if err == nil {
				conn := protocol.NewConnection(nc)
				conn.ID = crypto.GenerateID(crypto.CONN)

				lb.RWmu.Lock()
				lb.msConn[ms.ID] = conn
				lb.RWmu.Unlock()

				go conn.ReceiveLoopChan(lb)

				log["console"].Info("Connected to microservice: %s", address)
				return conn, nil
			}
		}
	}
}

func (lb *LoadBalancer) handleRecover(request protocol.Message) protocol.Message {
	var services map[protocol.ServiceType][]*protocol.MsInfo

	log["console"].Debug("ms string: %v", request.Content)
	err := json.DecodeContent(request.Content, &services)
	if err != nil {
		return protocol.Message{
			ID:      request.ID,
			Type:    request.Type,
			Code:    protocol.ERROR,
			Content: err.Error(),
		}
	}

	for _, mses := range services {
		for _, ms := range mses {
			msConn, err := lb.connectToMicroservice(ms)
			if err != nil {
				log["console"].Error("connecting to ms %v: %v", ms, err)
				break
			}

			//send test message to check if ms works
			resp, err := msConn.SendRequest(protocol.Message{
				ID:   crypto.GenerateID(crypto.MESSAGE_ID),
				Type: protocol.TEST,
			})

			if err != nil || resp.Code != protocol.SUCCESS {
				return protocol.Message{
					ID:           request.ID,
					Type:         protocol.UPDATE,
					ConnectionID: request.ConnectionID,
					Code:         protocol.ERROR,
					Content:      err.Error(),
				}
			}

			msConn.Mu.Lock()
			lb.msInfo[ms.Type] = append(lb.msInfo[ms.Type], ms)
			lb.msConn[ms.ID] = msConn
			msConn.Mu.Unlock()

			if ms.Process, err = os.FindProcess(ms.PID); err != nil {
				log["console"].Error("Finding ms process PID %v: %v", ms.PID, err)
				break
			}

			log["console"].Debug("Successfully recovered connection to ms %v", ms)
		}
	}

	return protocol.Message{
		ID:   request.ID,
		Type: request.Type,
		Code: protocol.SUCCESS,
	}
}

// ################################# NODE METHODS #################################

func (lb *LoadBalancer) ReceiveHeartBeat(msg protocol.Message, conn *protocol.Connection) {

}

func (lb *LoadBalancer) AsyncEvent(request protocol.Message, conn *protocol.Connection) {

	var response protocol.Message

	switch request.Type {
	case protocol.RECOVER:
		log["console"].Debug("######### Got recover message in async ###################\n")
		response = lb.handleRecover(request)
	//TODO: later extend this to parsing if update should add, delete or update ms data  (add - add, del - delete, nothing - just update) as a 6th part in msg
	case protocol.UPDATE:
		log["console"].Debug("Got update from controller")
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

			msConn, err := lb.connectToMicroservice(ms)
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

			//send test message to check if ms works
			resp, err := msConn.SendRequest(protocol.Message{
				ID:   crypto.GenerateID(crypto.MESSAGE_ID),
				Type: protocol.TEST,
			})

			if err != nil || resp.Code != protocol.SUCCESS {
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

	default:
		strErr := "unknown async command: " + string(request.Type) + "\n"
		log["console"].Error(strErr)
		response = protocol.Message{
			ID:      request.ID,
			Type:    request.Type,
			Code:    protocol.ERROR,
			Content: strErr,
		}
	}

	if err := protocol.Send(conn.RW.Writer, response); err != nil {
		log["console"].Error("Error sending response: %w\n", err)
		return
	}

}

func (lb *LoadBalancer) String() string {
	return "LoadBalancer"
}

// ################################# NODE METHODS #################################
