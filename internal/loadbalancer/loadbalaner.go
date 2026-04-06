package loadbalancer

import (
	"fmt"
	"net"
	crypto "networking/tcp/internal/cryptography"
	"networking/tcp/internal/protocol"
	"strings"
	"sync"
	"time"
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
	fmt.Println("[LBALANCER]: LoadBalancer listening for Controller on", lb.listener.Addr().String())

	conn, err := lb.listener.Accept()
	if err != nil {
		fmt.Println("[LBALANCER]: Accept error", err)
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
			fmt.Println("[LBALANCER]: Error while reading from controller", err)
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
				Type:         protocol.UPDATE,
				ConnectionID: request.ConnectionID,
				Code:         protocol.ERROR,
				Content:      err.Error(),
			}
			break
		}

		fmt.Printf("[LBALANCER]: Successfully added ms %s:%s type:%s\n", ms.Host, ms.Port, ms.Type)

		response = protocol.Message{Code: protocol.SUCCESS}

	case protocol.PING:
		fallthrough
	case protocol.DOWNLOAD:
		fallthrough
	case protocol.UPLOAD:
		//forward message to microservice
		var msType string
		msType = strings.ToLower(string(request.Type))

		fmt.Println("MS type: ", msType)

		//take first ms with this type
		services := lb.msInfo[msType]

		if len(services) < 1 {
			fmt.Println("[LBALANCER]: no services available")
			return
		}

		ms := services[0]

		fmt.Println("MS full ", ms.ID, ms.Host, ms.Port, ms.Type, ms.NodeID)

		msConn := lb.msConn[ms.ID]

		if msConn == nil {
			response = protocol.Message{
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
				Type:         request.Type,
				Code:         protocol.ERROR,
				ConnectionID: request.ConnectionID,
				Content:      err.Error(),
			}
		}

		fmt.Println("[LBALANCER]: response from ms: ", response)

	default:
		response = protocol.Message{
			Type: protocol.CREATE, Code: protocol.ERROR, Content: "unknown command: " + string(request.Type),
		}
	}

	if err := protocol.Send(conn.RW.Writer, response); err != nil {
		fmt.Println("[LBALANCER]: Error sending response:", err)
		return
	}
}

func parseMsFromMessage(msg protocol.Message) (*protocol.MsInfo, error) {
	if msg.Content == "" {
		return nil, fmt.Errorf("[LBALANCER]: Empty message while parsing ms")
	}

	fmt.Printf("[LBALANCER]: message content %s\n", msg.Content)

	dataSplit := strings.Split(msg.Content, ";")

	expectedSplitCount := 5
	if len(dataSplit) != expectedSplitCount {
		return nil, fmt.Errorf("[LBALANCER]: Expected %d values while parsing ms, got: %v", expectedSplitCount, dataSplit)
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
	nc, err := net.DialTimeout("tcp", net.JoinHostPort(ms.Host, ms.Port), 5*time.Second)
	if err != nil {
		return err
	}

	conn := protocol.NewConnection(nc)

	conn.ID = crypto.GenerateID()

	lb.msConn[ms.ID] = conn

	return nil
}
