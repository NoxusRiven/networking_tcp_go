package agent

import (
	"fmt"
	"net"
	crypto "networking/tcp/internal/cryptography"
	json "networking/tcp/internal/json_helper"
	"networking/tcp/internal/logger"
	"networking/tcp/internal/platform"
	"networking/tcp/internal/protocol"
	"os"
	"os/exec"
	"sync"
	"time"
)

var log logger.Loggers = logger.NewLoggers(
	logger.WithConsole(os.Stdout, os.Stderr),
	logger.WithBaseOptions(
		logger.PrefixField("agent"),
		logger.FormatField(logger.BASE_PREFIX),
	),
)

const (
	BASE_PORT_MS uint16 = 20000
)

type Agent struct {
	//key is Conn ID
	msInfo map[string]*protocol.MsInfo
	//key is MsInfo ID
	msConn map[string]*protocol.Connection

	listener net.Listener

	RWmu sync.RWMutex

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
		msInfo:        make(map[string]*protocol.MsInfo),
		msConn:        make(map[string]*protocol.Connection),
		nextPortCount: 0,
	}, nil

}

func (a *Agent) Start() {
	log["console"].Debug("Agent listening for Controller on %s", a.listener.Addr().String())

	nc, err := a.listener.Accept()
	if err != nil {
		log["console"].Debug("Accept error %w", err)
		return
	}

	a.handleControllerConnection(nc)
}

func (a *Agent) handleControllerConnection(nc net.Conn) {
	conn := protocol.NewConnection(nc)
	defer conn.Close()

	go conn.ReceiveLoop(a)

	go a.SendHeartBeat(conn)

	select {}
}

func (a *Agent) SendHeartBeat(conn *protocol.Connection) {

	msg := protocol.Message{
		Type:    protocol.HEARTBEAT,
		Content: "AGENT",
	}

	for {
		time.Sleep(5 * time.Second)

		msg.ID = crypto.GenerateID(crypto.MESSAGE_ID)
		if err := protocol.Send(conn.RW.Writer, msg); err != nil {
			log["console"].Error("Error sending heartbeat: %v", err)
		}
	}
}

func (a *Agent) GetNextPort() string {
	num := a.nextPortCount
	a.nextPortCount++

	return fmt.Sprintf("%d", BASE_PORT_MS+num)
}

func (a *Agent) createMicroservice(host string, port string, ms_type string) (*protocol.MsInfo, error) {

	//Correct exec Command
	cmd := exec.Command(
		platform.Executable("service"),
		"--port", port,
		"--type", ms_type,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Start()
	if err != nil {
		log["console"].Debug("Error while starting ms process %w", err)
		return nil, logger.StrToError(log["string"], func() {
			log["string"].Error("error start ms process: %w", err)
		})
	}

	log["console"].Info("ms %s process started successfully! Pid: %d", ms_type, cmd.Process.Pid)

	ms := &protocol.MsInfo{
		ID:      crypto.GenerateID(crypto.INSTANCE_NODE),
		PID:     cmd.Process.Pid,
		Host:    host,
		Port:    port,
		Process: cmd.Process,
		Type:    protocol.ServiceType(ms_type),
	}

	//check if connection works
	msConn, err := a.connectToMicroservice(ms)
	if err != nil {
		return nil, logger.StrToErrorNew(log, "Error while handling ms connection: %v", err)
	}

	a.RWmu.Lock()
	log["console"].Debug("saving ms conn with ms id %v and ms info with conn id %v", ms.ID, msConn.ID)
	a.msInfo[msConn.ID] = ms
	a.msConn[ms.ID] = msConn
	a.RWmu.Unlock()

	return ms, nil
}

func (a *Agent) KillAllMS() {
	for _, ms := range a.msInfo {
		if ms == nil || ms.Process == nil {
			continue
		}
		ms.Process.Kill()

	}
}

func (a *Agent) connectToMicroservice(ms *protocol.MsInfo) (*protocol.Connection, error) {

	var nc net.Conn
	var err error
	if nc, err = net.Dial("tcp", net.JoinHostPort(ms.Host, ms.Port)); err != nil {
		return nil, logger.StrToErrorNew(log, "Error while trying to connect to microservice %v", ms)
	}

	msConn := protocol.NewConnection(nc)
	msConn.ID = crypto.GenerateID(crypto.CONN)

	go msConn.ReceiveLoop(a)

	resp, err := msConn.SendRequest(protocol.Message{
		ID:      crypto.GenerateID(crypto.MESSAGE_ID),
		Type:    protocol.HEARTBEAT,
		Content: "AGENT",
	})

	if err != nil || resp.Type != protocol.HEARTBEAT || resp.Code != protocol.SUCCESS {
		return nil, logger.StrToErrorNew(log, "Error when recived response to test heartbeat: %v", resp)
	}

	log["console"].Debug("Successfully connected to ms: %v", ms)

	return msConn, nil
}

func (a *Agent) handleCreate(request protocol.Message) protocol.Message {
	serviceType := request.Content.(string) // e.g. "PING" — controller sends type in Content
	a.RWmu.Lock()
	port := a.GetNextPort()
	a.RWmu.Unlock()

	ms, err := a.createMicroservice("localhost", port, serviceType)
	if err != nil {
		return protocol.Message{
			ID: request.ID, Type: protocol.CREATE, Code: protocol.ERROR, Content: err.Error(),
		}
	}

	return protocol.Message{
		ID: request.ID, Type: protocol.CREATE, Code: protocol.SUCCESS,
		Content: net.JoinHostPort(ms.Host, ms.Port),
	}
}

func (a *Agent) handleRecover(request protocol.Message) protocol.Message {
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
			msConn, err := a.connectToMicroservice(ms)
			if err != nil {
				log["console"].Error("connecting to ms %v: %v", ms, err)
				continue
			}

			msConn.Mu.Lock()
			a.msInfo[msConn.ID] = ms
			a.msConn[ms.ID] = msConn
			msConn.Mu.Unlock()

			if ms.Process, err = os.FindProcess(ms.PID); err != nil {
				log["console"].Error("Finding ms process PID %v: %v", ms.PID, err)
				continue
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

func (a *Agent) ReceiveHeartBeat(msg protocol.Message, conn *protocol.Connection) {
	a.RWmu.Lock()
	defer a.RWmu.Unlock()
	log["console"].Debug("conn id: %v", conn.ID)
	if ms, ok := a.msInfo[conn.ID]; ok {
		log["console"].Debug("Microservice %v heartbeat was %v", ms.ID, ms.LastHeartbeat)
		ms.Mu.Lock()
		ms.LastHeartbeat = time.Now()
		ms.Status = protocol.Healthy
		ms.Mu.Unlock()
		log["console"].Debug("Microservice %v heartbeat is %v", ms.ID, ms.LastHeartbeat)

	} else {
		log["console"].Error("Unknown agent in connection with id: %v", conn.ID)
	}
}

func (a *Agent) AsyncEvent(request protocol.Message, conn *protocol.Connection) {

	var response protocol.Message

	switch request.Type {
	case protocol.CREATE:
		response = a.handleCreate(request)
	case protocol.RECOVER:
		response = a.handleRecover(request)

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

func (a *Agent) String() string {
	return "Agent"
}

// ################################# NODE METHODS #################################
