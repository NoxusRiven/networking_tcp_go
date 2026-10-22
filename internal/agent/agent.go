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
	nodeID string

	controllerConnection *protocol.Connection

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
func NewAgent(lisPort string, nodeID string) (*Agent, error) {
	//listen, err := net.Listen("tcp", ":0")
	listen, err := net.Listen("tcp", lisPort)
	if err != nil {
		return nil, err
	}

	return &Agent{
		nodeID:        nodeID,
		listener:      listen,
		msInfo:        make(map[string]*protocol.MsInfo),
		msConn:        make(map[string]*protocol.Connection),
		nextPortCount: 0,
	}, nil

}

// for now agent only listens for controller
func (a *Agent) Start() {
	log["console"].Debug("Agent listening for Controller on %s", a.listener.Addr().String())

	nc, err := a.listener.Accept()
	if err != nil {
		log["console"].Debug("Accept error %w", err)
		return
	}

	a.handleConnection(nc)
}

func (a *Agent) handleConnection(nc net.Conn) {
	conn := protocol.NewConnection(nc)
	defer conn.Close()

	msg, err := protocol.Receive(conn.RW.Reader)
	if err != nil {
		log["console"].Error("Receiving handshake message: %v", err)
		return
	} else if msg.Type != protocol.HELLO {
		log["console"].Error("Wrong message type during handshake: %v", msg)
		return
	}

	if msg.Content == "CONTROLLER" {
		a.handleController(conn)
	} else {
		log["console"].Error("Unknow node connected %v", msg)
		return
	}

	protocol.Send(conn.RW.Writer, protocol.Message{
		ID:      msg.ID,
		Type:    msg.Type,
		Code:    protocol.SUCCESS,
		Content: a.String(),
	})

	select {}
}

func (a *Agent) handleController(conn *protocol.Connection) {

	a.controllerConnection = conn

	go conn.ReceiveLoop(a)

	go a.SendHeartBeat(conn)
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

	//Create exec Command
	cmd := exec.Command(
		platform.Executable("service"),
		"--port", port,
		"--type", ms_type,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Start()
	if err != nil {
		log["console"].Error("Error while starting ms process %v", err)
		return nil, logger.StrToErrorNew(log, "error start ms process: %v", err)
	}

	log["console"].Info("ms %s process started successfully! Pid: %d", ms_type, cmd.Process.Pid)

	ms := &protocol.MsInfo{
		ID:            crypto.GenerateID(crypto.INSTANCE_NODE),
		NodeID:        a.nodeID,
		PID:           cmd.Process.Pid,
		Host:          host,
		Port:          port,
		LastHeartbeat: time.Now(),
		Process:       cmd.Process,
		Type:          protocol.ServiceType(ms_type),
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

	//set heartbeat after connection to prevents infitite "Unhealty loop"
	ms.LastHeartbeat = time.Now()

	go msConn.ReceiveLoop(a)

	go a.watchMsHealth(ms)

	resp, err := msConn.SendRequest(protocol.Message{
		ID:      crypto.GenerateID(crypto.MESSAGE_ID),
		Type:    protocol.HELLO,
		Content: a.String(),
	})

	//TODO: also check if node isnt a microservice
	if err != nil || resp.Type != protocol.HELLO || resp.Code != protocol.SUCCESS {
		return nil, logger.StrToErrorNew(log, "Error when recived response to handshake hello: %v", resp)
	}

	log["console"].Debug("Successfully connected to ms: %v", ms)

	ms.LastHeartbeat = time.Now()

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
			ID:      request.ID,
			Type:    protocol.CREATE,
			Code:    protocol.ERROR,
			Content: err.Error(),
		}
	}

	msJson, err := json.ToJSON(ms)
	if err != nil {
		return protocol.Message{
			ID:      request.ID,
			Type:    protocol.CREATE,
			Code:    protocol.ERROR,
			Content: err.Error(),
		}
	}

	return protocol.Message{
		ID:   request.ID,
		Type: protocol.CREATE,
		Code: protocol.SUCCESS,
		Content:/*net.JoinHostPort(ms.Host, ms.Port)*/ msJson,
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
				break
			}

			msConn.Mu.Lock()
			a.msInfo[msConn.ID] = ms
			a.msConn[ms.ID] = msConn
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

func (a *Agent) watchMsHealth(ms *protocol.MsInfo) {
	allowedDelay := 7 * time.Second

	for {
		timestamp := ms.LastHeartbeat
		if time.Since(timestamp) > allowedDelay {
			ms.Status = protocol.Unhealthy
			log["console"].Debug("MS %v has status UNHEALTHY", ms.ID)

			go a.restartMs(ms)
			return
		}

		time.Sleep(1 * time.Second)
	}
}

func (a *Agent) restartMs(ms *protocol.MsInfo) {

	//Both of errors are more of an debug if process and connection were already finished
	if err := ms.Process.Kill(); err != nil {
		log["console"].Debug("killing microservice %v process: %v", ms.ID, err)
	}

	prevConn := a.msConn[ms.ID]
	if err := prevConn.Nc.Close(); err != nil {
		log["console"].Debug("close previous microservice connection: %v", err)
	}

	cmd := exec.Command(
		platform.Executable("service"),
		"--port", ms.Port,
		"--type", string(ms.Type),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Start()
	if err != nil {
		log["console"].Error("Error while starting ms process %v", err)
		return
	}

	log["console"].Info("Restarted ms process %s-%s successfully! Pid: %d", ms.ID, ms.Type, ms.Process.Pid)

	msConn, err := a.connectToMicroservice(ms)
	if err != nil {
		log["console"].Error("Connection to ms id-%v: %v", ms.ID, err)
		return
	}

	msConn.Mu.Lock()
	a.msConn[ms.ID] = msConn
	a.msInfo[msConn.ID] = ms
	msConn.Mu.Unlock()
}

// ################################# NODE METHODS #################################
func (a *Agent) HandShake(conn *protocol.Connection) {

}

func (a *Agent) ReceiveHeartBeat(msg protocol.Message, conn *protocol.Connection) {
	a.RWmu.Lock()
	defer a.RWmu.Unlock()
	if ms, ok := a.msInfo[conn.ID]; ok {
		ms.Mu.Lock()
		ms.LastHeartbeat = time.Now()
		ms.Status = protocol.Healthy
		ms.Mu.Unlock()
		log["console"].Debug("Microservice %v heartbeat is %v", ms.ID, ms.LastHeartbeat)

	} else {
		log["console"].Error("Unknown microservice in connection with id: %v", conn.ID)
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
			ID: request.ID, Type: request.Type, Code: protocol.ERROR, Content: "unknown async command: " + string(request.Type),
		}
	}

	if err := protocol.Send(conn.RW.Writer, response); err != nil {
		log["console"].Error("Error sending response: %w", err)
		return
	}
}

func (a *Agent) String() string {
	return "AGENT"
}

// ################################# NODE METHODS #################################
