package agent

import (
	"fmt"
	"net"
	crypto "networking/tcp/internal/cryptography"
	"networking/tcp/internal/protocol"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	BASE_PORT_MS uint16 = 20000
)

type Agent struct {
	//key is service type
	msInfo map[string][]*protocol.MsInfo
	//key is ID
	msConn map[string]*protocol.Connection

	listener net.Listener

	mu sync.RWMutex

	//adding to base port number for ms
	nextPortCount uint16
}

// TODO: handle getting free port and communicating it to controller
func NewAgent(lisPort string) (*Agent, error) {
	//listen, err := net.Listen("tcp", ":0")
	listen, err := net.Listen("tcp", lisPort)
	if err != nil {
		return nil, err
	}

	return &Agent{
		listener:      listen,
		msInfo:        make(map[string][]*protocol.MsInfo),
		nextPortCount: 0,
	}, nil

}

func (a *Agent) Start() {
	fmt.Println("[AGENT]: Agent listening for Controller on", a.listener.Addr().String())

	conn, err := a.listener.Accept()
	if err != nil {
		fmt.Println("[AGENT]: Accept error", err)
		return
	}

	go a.handleControllerConnection(conn)

}

func (a *Agent) handleControllerConnection(nc net.Conn) {
	conn := protocol.NewConnection(nc)
	defer conn.Close()

	for {
		request, err := protocol.Receive(conn.RW.Reader)
		if err != nil {
			fmt.Println("[AGENT]: Error while reading from controller", err)
			return
		}

		a.handleControllerRequest(conn, request)

	}
}

func (a *Agent) handleControllerRequest(conn *protocol.Connection, request protocol.Message) {
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

	if err := protocol.Send(conn.RW.Writer, response); err != nil {
		fmt.Println("[AGENT]: Error sending response:", err)
		return
	}
}

func (a *Agent) GetNextPort() string {
	num := a.nextPortCount
	a.nextPortCount++

	return fmt.Sprintf("%d", BASE_PORT_MS+num)
}

func (a *Agent) createMicroservice(host string, port string, ms_type string) (*protocol.MsInfo, error) {

	//Correct exec Command
	cmd := exec.Command("../../cmd/microservice/service.exe", "--port", port, "--type", ms_type)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Start()
	if err != nil {
		fmt.Println("[AGENT]: Error while starting ms process", err)
		return nil, fmt.Errorf("[AGENT]: error start ms process: %w", err)
	}

	fmt.Printf("[AGENT]: ms %s process started successfully! Pid: %d", ms_type, cmd.Process.Pid)

	ms := &protocol.MsInfo{
		Host: host,
		Port: port,
		Type: ms_type,
	}

	if err := a.connectToMicroservice(ms); err != nil {
		return nil, err
	}

	// TODO: healthCheck(ms) when microservice implements it

	a.mu.Lock()
	a.msInfo[ms_type] = append(a.msInfo[ms_type], ms)
	a.mu.Unlock()

	return ms, nil
}

func (a *Agent) connectToMicroservice(ms *protocol.MsInfo) error {
	nc, err := net.DialTimeout("tcp", net.JoinHostPort(ms.Host, ms.Port), 5*time.Second)
	if err != nil {
		return err
	}

	conn := protocol.NewConnection(nc)

	conn.ID = crypto.GenerateID()

	a.msConn[conn.ID] = conn

	return nil
}

func (a *Agent) KillAllMS() {
	for _, ms := range a.msInfo {
		for _, m := range ms {
			if m == nil || m.Cmd == nil || m.Cmd.Process == nil {
				continue
			}
			m.Cmd.Process.Kill()
		}

	}
}

func healthCheck() error {
	return nil
}
