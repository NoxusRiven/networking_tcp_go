package agent

import (
	"fmt"
	"net"
	crypto "networking/tcp/internal/crypto"
	json "networking/tcp/internal/json"
	"networking/tcp/internal/logger"
	"networking/tcp/internal/platform"
	"networking/tcp/internal/protocol"
	"os"
	"os/exec"
	"strconv"
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
		nextPortCount: BASE_PORT_MS,
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

	log["console"].Info("Accepted connection")

	a.handleConnection(nc)
}

func (a *Agent) handleConnection(nc net.Conn) {
	defer a.KillAllMS()
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
		log["console"].Debug("Found controller")
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

	return fmt.Sprintf("%d", num)
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
		return nil, logger.StrToError(log, "error start ms process: %v", err)
	}

	log["console"].Info("ms %s process started successfully! Pid: %d", ms_type, cmd.Process.Pid)

	ms := &protocol.MsInfo{
		Base: protocol.NodeBase{
			ID:            crypto.GenerateID(crypto.INSTANCE_NODE),
			NodeID:        a.nodeID,
			Host:          host,
			Port:          port,
			LastHeartbeat: time.Now(),
		},
		PID:     cmd.Process.Pid,
		Process: cmd.Process,
		Type:    protocol.ServiceType(ms_type),
	}

	//check if connection works
	err = a.connectToMicroservice(ms)
	if err != nil {
		return nil, logger.StrToError(log, "Error while handling ms connection: %v", err)
	}

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

func (a *Agent) connectToMicroservice(ms *protocol.MsInfo) error {

	msConn, err := protocol.TryToConnect(net.JoinHostPort(ms.Base.Host, ms.Base.Port), 5*time.Second)
	if err != nil {
		return logger.StrToError(log, "Error while trying to connect to microservice %v", ms)
	}

	//set heartbeat after connection to prevents infitite "Unhealty loop"
	ms.Base.LastHeartbeat = time.Now()

	go msConn.ReceiveLoop(a)

	go a.watchMsHealth(ms)

	resp, err := msConn.SendRequest(protocol.Message{
		ID:      crypto.GenerateID(crypto.MESSAGE_ID),
		Type:    protocol.HELLO,
		Content: a.String(),
	})

	//TODO: also check if node isnt a microservice
	if err != nil || !resp.IsCorrect(protocol.HELLO) {
		return logger.StrToError(log, "Error when recived response to handshake hello: %v", resp)
	}

	msConn.Mu.Lock()

	ms.Base.LastHeartbeat = time.Now()
	a.msConn[ms.Base.ID] = msConn
	a.msInfo[msConn.ID] = ms

	msConn.Mu.Unlock()

	log["console"].Debug("Successfully connected to ms: %v", ms)

	return nil
}

func (a *Agent) handleCreate(request protocol.Message) protocol.Message {
	serviceType := request.Content // e.g. "PING" — controller sends type in Content
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
		ID:      request.ID,
		Type:    protocol.CREATE,
		Code:    protocol.SUCCESS,
		Content: msJson,
	}
}

func (a *Agent) handleRecover(request protocol.Message) protocol.Message {
	//var services map[protocol.ServiceType][]*protocol.MsInfo

	log["console"].Debug("ms string: %v", request.Content)
	//err := json.DecodeContent(request.Content, &services)
	type Data struct {
		Services map[protocol.ServiceType][]*protocol.MsInfo `json:"services"`
		Port     string                                      `json:"port"`
	}

	agentRecover, err := json.FromJSON[Data](request.Content)
	if err != nil {
		return protocol.Message{
			ID:      request.ID,
			Type:    request.Type,
			Code:    protocol.ERROR,
			Content: err.Error(),
		}
	}

	uintConv, err := strconv.ParseUint(agentRecover.Port, 10, 16)
	if err != nil {
		log["console"].Error("Convert strint to uint16")
		return protocol.Message{
			ID:      request.ID,
			Type:    request.Type,
			Code:    protocol.ERROR,
			Content: err.Error(),
		}
	}

	//controller gives current gotta +1
	a.nextPortCount = uint16(uintConv) + 1

	services := agentRecover.Services
	//gather ms that failed to connect
	var errList []string

	for _, mses := range services {
		for _, ms := range mses {
			err := a.connectToMicroservice(ms)
			if err != nil {
				log["console"].Warning("connecting to ms %v: %v", ms, err)
				log["console"].Info("Restarting ms: %v-%v", ms.Base.ID, ms.Type)

				if err = a.restartMs(ms); err != nil {
					log["console"].Error("Restart ms %v: %v", ms.Base.ID, err)
					errList = append(errList, ms.Base.ID)
					continue
				}
			}

			if ms.Process, err = os.FindProcess(ms.PID); err != nil {
				log["console"].Error("Finding ms process PID %v: %v", ms.PID, err)
				errList = append(errList, ms.Base.ID)
				continue
			}

			log["console"].Debug("Successfully recovered connection to ms %v", ms)
		}
	}

	code := protocol.SUCCESS
	var data string
	if len(errList) > 0 {
		code = protocol.ERROR
		data, err = json.ToJSON(errList)
	}

	return protocol.Message{
		ID:      request.ID,
		Type:    request.Type,
		Code:    code,
		Content: data,
	}
}

func (a *Agent) watchMsHealth(ms *protocol.MsInfo) {
	allowedDelay := 6 * time.Second

	for {
		timestamp := ms.Base.LastHeartbeat
		if time.Since(timestamp) > allowedDelay {
			ms.Base.Status = protocol.Unhealthy
			log["console"].Debug("MS %v has status UNHEALTHY", ms.Base.ID)

			go a.restartMs(ms)
			//a.restartMs(ms)
			return
		}

		time.Sleep(1 * time.Second)
	}
}

func (a *Agent) restartMs(ms *protocol.MsInfo) error {

	strErr := ""
	var prevConn *protocol.Connection
	if ms.Process == nil {
		goto newMS
	}

	//Both of errors are more of an debug if process and connection were already finished
	if err := ms.Process.Kill(); err != nil {
		log["console"].Warning("killing microservice %v process: %v", ms.Base.ID, err)
	}

	prevConn = a.msConn[ms.Base.ID]
	if err := prevConn.Nc.Close(); err != nil {
		log["console"].Warning("close previous microservice connection: %v", err)
	}

newMS:
	cmd := exec.Command(
		platform.Executable("service"),
		"--port", ms.Base.Port,
		"--type", string(ms.Type),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Start()
	if err != nil {
		strErr = fmt.Sprintf("Error while starting ms process %v", err)
		log["console"].Error(strErr)
		return logger.StrToError(log, strErr)
	}

	ms.Process = cmd.Process
	ms.PID = ms.Process.Pid

	log["console"].Info("Restarted ms process %s-%s successfully! Pid: %d", ms.Base.ID, ms.Type, ms.Process.Pid)

	err = a.connectToMicroservice(ms)
	if err != nil {
		strErr = fmt.Sprintf("Connection to ms id-%v: %v", ms.Base.ID, err)
		log["console"].Error(strErr)
		return logger.StrToError(log, strErr)
	}

	return nil
}

// ################################# NODE METHODS #################################
func (a *Agent) HandShake(conn *protocol.Connection) {

}

func (a *Agent) ReceiveHeartBeat(msg protocol.Message, conn *protocol.Connection) {
	a.RWmu.Lock()
	defer a.RWmu.Unlock()
	if ms, ok := a.msInfo[conn.ID]; ok {
		ms.Mu.Lock()
		ms.Base.LastHeartbeat = time.Now()
		ms.Base.Status = protocol.Healthy
		ms.Mu.Unlock()
		log["console"].Debug("Microservice %v heartbeat is %v", ms.Base.ID, ms.Base.LastHeartbeat)

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
