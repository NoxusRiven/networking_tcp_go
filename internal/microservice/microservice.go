package microservice

import (
	"bufio"
	"fmt"
	"net"
	"networking/tcp/internal/protocol"
	"time"
)

type Microservice struct {
	listener net.Listener
}

func NewMicroservice(listenerPort string) (*Microservice, error) {
	listen, err := net.Listen("tcp", listenerPort)
	if err != nil {
		return nil, err
	}

	return &Microservice{
		listener: listen,
	}, nil
}

func (ms *Microservice) Start(serviceType string) {
	fmt.Printf("[SERVICE]: service %s started", serviceType)
	switch serviceType {
	case "ping":
		ms.acceptConnections(ms.pingService)
	default:
		fmt.Println("[SERVICE]: unknow service type", serviceType)
	}
}

func (ms *Microservice) acceptConnections(serviceFunc func(conn net.Conn)) {
	for {
		conn, err := ms.listener.Accept()
		if err != nil {
			fmt.Println("[SERVICE]: accepting connection error", err)
			return
		}

		go serviceFunc(conn)
	}
}

func (ms *Microservice) pingService(conn net.Conn) {
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for {
		request, err := protocol.Receive(reader)
		if err != nil {
			fmt.Println("[SERVICE]: reading request error", err)
			return
		}

		fmt.Println(request)

		curr_time := time.Now()

		response := protocol.Message{
			SessionID:    request.SessionID,
			ConnectionID: request.ConnectionID,
			Type:         request.Type,
			Code:         protocol.SUCCESS,
			Content:      curr_time.String(),
		}

		err = protocol.Send(writer, response)
		if err != nil {
			fmt.Println("[SERVICE]: sending response error", err)
			return
		}
	}
}

func (ms *Microservice) loginService() {

}
