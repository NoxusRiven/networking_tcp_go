package agent

import (
	"bufio"
	"fmt"
	"net"
	"networking/tcp/internal/protocol"
	"os/exec"
	"sync"
	"time"
)

const (
	BASE_PORT_MS uint16 = 20000
)

type Agent struct {
	microservices map[string][]*protocol.MsInfo
	listener      net.Listener

	mu sync.RWMutex

	//adding to base port number for ms
	nextPortCount uint16
}

func NewAgent(listenPort string) (*Agent, error) {
	listen, err := net.Listen("tcp", listenPort)
	if err != nil {
		return nil, err
	}

	return &Agent{
		listener:      listen,
		microservices: make(map[string][]*protocol.MsInfo),
		nextPortCount: 0,
	}, nil

}

func (a *Agent) Start() {
	fmt.Println("Agent listening for Controller on", a.listener.Addr().String())

	conn, err := a.listener.Accept()
	if err != nil {
		fmt.Println("Accept error", err)
		return
	}

	go a.handleControllerConnection(conn)

}

func (a *Agent) handleControllerConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for {
		request, err := protocol.Receive(reader)
		if err != nil {
			fmt.Println("Error while reading from controller", err)
			return
		}

		var response protocol.Message

		switch request.Type {
		case protocol.CREATE:
			serviceType := request.Content // e.g. "PING" — controller sends type in Content
			a.mu.Lock()
			port := a.GetNextPort()
			a.mu.Unlock()

			ms, err := a.createMicroservice("localhost", port, serviceType)
			if err != nil {
				response = protocol.Message{
					Type: protocol.CREATE, Code: protocol.ERROR, Content: err.Error(),
				}
				break
			}

			response = protocol.Message{
				Type: protocol.CREATE, Code: protocol.SUCCESS,
				Content: net.JoinHostPort(ms.Host, ms.Port),
			}

		default:
			response = protocol.Message{
				Type: protocol.CREATE, Code: protocol.ERROR, Content: "unknown command: " + string(request.Type),
			}
		}

		if err := protocol.Send(writer, response); err != nil {
			fmt.Println("Error sending response:", err)
			return
		}
	}
}

func (a *Agent) GetNextPort() string {
	num := a.nextPortCount
	a.nextPortCount++

	return fmt.Sprintf("%d", BASE_PORT_MS+num)
}

func (a *Agent) createMicroservice(host string, port string, ms_type string) (*protocol.MsInfo, error) {
	cmd := exec.Command("./tcp/cmd/microservice", "--port", port, "--type", ms_type)

	err := cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("start ms process: %w", err)
	}

	ms := &protocol.MsInfo{
		Host: host,
		Port: port,
	}

	if err := connectToMicroservice(ms); err != nil {
		return nil, err
	}

	// TODO: healthCheck(ms) when microservice implements it

	a.mu.Lock()
	a.microservices[ms_type] = append(a.microservices[ms_type], ms)
	a.mu.Unlock()

	return ms, nil
}

func connectToMicroservice(ms *protocol.MsInfo) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ms.Host, ms.Port), 5*time.Second)
	if err != nil {
		return err
	}

	ms.Conn = conn
	ms.RW = bufio.NewReadWriter(
		bufio.NewReader(conn),
		bufio.NewWriter(conn),
	)

	return nil
}

func healthCheck(ms *protocol.MsInfo) error {
	request := protocol.Message{
		Type: protocol.HEARTBEAT,
	}

	err := protocol.Send(ms.RW.Writer, request)
	if err != nil {
		return err
	}

	var response protocol.Message
	response, err = protocol.Receive(ms.RW.Reader)
	if err != nil {
		return err
	}

	if response.Content != "200" {
		return fmt.Errorf("Healthcheck was not successful")
	}

	return nil

}
