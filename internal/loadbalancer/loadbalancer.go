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
	msInfo map[string][]*protocol.MsInfo
	msConn map[string]*protocol.Connection

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
		msInfo:   make(map[string][]*protocol.MsInfo),
		msConn:   make(map[string]*protocol.Connection),
	}, nil
}

func (lb *LoadBalancer) Start() {
	log["console"].Info("listening for Controller on %s", lb.listener.Addr().String())

	conn, err := lb.listener.Accept()
	if err != nil {
		log["console"].Error("Accept error: %w", err)
		return
	}

	go lb.handleControllerConnection(conn)

}

func (lb *LoadBalancer) handleControllerConnection(nc net.Conn) {
	conn := protocol.NewConnection(nc)
	defer conn.Close()

	for {
		request, err := protocol.Receive(conn.RW.Reader)
		if err != nil {
			log["console"].Error("Error while reading from controller: %w", err)
			return
		}

		lb.handleControllerRequest(conn, request)

	}
}

func (lb *LoadBalancer) handleControllerRequest(conn *protocol.Connection, request protocol.Message) {
	var response protocol.Message
	var err error

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

		lb.msInfo[ms.Type] = append(lb.msInfo[ms.Type], ms)

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

		fmt.Printf("[LBALANCER]: Successfully added ms %s:%s type:%s\n", ms.Host, ms.Port, ms.Type)

		response = protocol.Message{ID: request.ID, Type: request.Type, Code: protocol.SUCCESS}

	case protocol.PING:
		fallthrough
	case protocol.DOWNLOAD:
		fallthrough
	case protocol.UPLOAD:
		//forward message to microservice
		var msType string
		msType = strings.ToLower(string(request.Type))

		//take first ms with this type
		services := lb.msInfo[msType]

		if len(services) < 1 {
			log["console"].Error("no services available")
			return
		}

		ms := services[0]

		msConn := lb.msConn[ms.ID]

		if msConn == nil {
			response = protocol.Message{
				ID:           request.ID,
				Type:         request.Type,
				Code:         protocol.ERROR,
				ConnectionID: request.ConnectionID,
				Content:      "Connection for ms is nil",
			}
		}

		protocol.Send(msConn.RW.Writer, request)

		response, err = protocol.Receive(msConn.RW.Reader)
		if err != nil {
			response = protocol.Message{
				ID:           request.ID,
				Type:         request.Type,
				Code:         protocol.ERROR,
				ConnectionID: request.ConnectionID,
				Content:      err.Error(),
			}
		}

		log["console"].Debug("response from ms: %s", response)

	default:
		response = protocol.Message{
			ID: request.ID, Type: protocol.CREATE, Code: protocol.ERROR, Content: "unknown command: " + string(request.Type),
		}
	}

	if err := protocol.Send(conn.RW.Writer, response); err != nil {
		log["console"].Error("Error sending response: %w", err)
		return
	}
}

func parseMsFromMessage(msg protocol.Message) (*protocol.MsInfo, error) {
	if msg.Content == "" {
		err := logger.StrToError(log["string"], func() {
			log["string"].Error("Empty message while parsing ms")
		})
		return nil, err
	}

	fmt.Printf("[LBALANCER]: message content %s\n", msg.Content)

	dataSplit := strings.Split(msg.Content, ";")

	expectedSplitCount := 5
	if len(dataSplit) != expectedSplitCount {
		err := logger.StrToError(log["string"], func() {
			log["string"].Error("Expected %d values while parsing ms, got: %v", expectedSplitCount, dataSplit)
		})
		return nil, err
	}

	fmt.Printf("Created ms (%s %s %s %s %s)", dataSplit[0], dataSplit[1], dataSplit[2], dataSplit[3], dataSplit[4])

	return &protocol.MsInfo{
		ID:     dataSplit[0],
		Host:   dataSplit[1],
		Port:   dataSplit[2],
		NodeID: dataSplit[3],
		Type:   dataSplit[4],
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

				log["console"].Info("Connected to microservice: %s", address)
				return nil
			}
		}
	}
}
