package controller

import (
	"fmt"
	"net"
	"networking/tcp/internal/config"
	"networking/tcp/internal/crypto"
	"networking/tcp/internal/json"
	"networking/tcp/internal/logger"
	"networking/tcp/internal/platform"
	"networking/tcp/internal/protocol"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

const (
	CACHE_FILE = "controller.cache.json"
	//TMP_CACHE_FILE = "controller.cache.json.tmp"
)

var log logger.Loggers = logger.NewLoggers(
	logger.WithConsole(os.Stdout, os.Stderr),
	logger.WithBaseOptions(
		logger.PrefixField("controller"),
		logger.FormatField(logger.BASE_PREFIX),
	),
)

// ##################################### STRUCTURES #####################################
// TODO: controller should update "lastHeartbeat" of nodes when they send hearthbeat and with every interaction
// TODO: only add second connection and use it when working with files
// TODO: if agent is in the same host as controller he can choose ports otherwise controller sets boundry or just a free port and agents sends back wich port he got in remote host
type Controller struct {
	//constant values
	BASE_PORT_AGENT string
	HOST_AGENT      string

	BASE_PORT_LBALANCER string
	HOST_LBALANCER      string

	PORT string

	listener net.Listener

	//? if controller will connect to more then 1 api make it same as agent storage
	apiConn *protocol.Connection

	AgentInfo  map[string]*protocol.AgentInfo `json:"agents"`
	agentsConn map[string]*protocol.Connection

	LbInfo map[string]*protocol.LBalancerInfo `json:"lbs"`
	lbConn map[string]*protocol.Connection

	Microservices map[protocol.ServiceType][]*protocol.MsInfo `json:"services"`

	//TODO: store list of available hosts

	mu sync.RWMutex

	NextID uint32 `json:"nextID"` // id tracker of agent and load balancer
}

// ##################################### STRUCTURES #####################################

// ################################## CORE FUNCTIONS #####################################
func NewController() *Controller {

	return &Controller{
		AgentInfo:  make(map[string]*protocol.AgentInfo),
		agentsConn: make(map[string]*protocol.Connection),

		LbInfo: make(map[string]*protocol.LBalancerInfo),
		lbConn: make(map[string]*protocol.Connection),

		Microservices: make(map[protocol.ServiceType][]*protocol.MsInfo),

		NextID: 0,
	}
}

func (c *Controller) setup() (error, bool) {
	//if cache file exists dont start from scratch so just return
	err := c.recoverStateFromFile()
	if err == nil {
		return nil, true
	}

	log["console"].Warning("Recover state from file: %v", err)

	//get config
	conf, err := config.ReadConfigFile()
	if err != nil {
		return err, false
	}

	//set values
	agentConf, err := conf.FindNodeByName(config.AGENT_CONF)
	if err != nil {
		return err, false
	}
	c.BASE_PORT_AGENT = agentConf.Port
	c.HOST_AGENT = agentConf.Host

	lbConf, err := conf.FindNodeByName(config.LB_CONF)
	if err != nil {
		return err, false
	}
	c.BASE_PORT_LBALANCER = lbConf.Port
	c.HOST_LBALANCER = lbConf.Host

	controllerConf, err := conf.FindNodeByName(config.CONTROLLER_CONF)
	if err != nil {
		return err, false
	}
	c.PORT = controllerConf.Port

	return nil, false
}

func (c *Controller) run() error {
	apiListener, err := net.Listen("tcp", ":"+c.PORT)
	if err != nil {
		return err
	}

	c.listener = apiListener

	log["console"].Info("Controller started [::]:%v", c.PORT)

	go c.acceptConnection()

	go c.cacheStateToFile()

	return nil
}

// starts and accepts api and system connections
func (c *Controller) Start() error {

	err, recovered := c.setup()
	if err != nil {
		return err
	}

	if !recovered {
		if err := c.run(); err != nil {
			return err
		}
	}

	return nil
}

// Node id is unused for API
func (c *Controller) Register(conn *protocol.Connection, connType protocol.ConnectionType, nodeID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	//TODO: make it that nodes generate thier id and report back to controller wich id they have
	conn.ID = crypto.GenerateID(crypto.CONN)

	switch connType {
	case protocol.ConnAPI:
		c.apiConn = conn
	case protocol.ConnAgent:
		c.agentsConn[nodeID] = conn
	case protocol.ConnLB:
		c.lbConn[nodeID] = conn
	default:
		log["console"].Error("Not able to register node ", connType)
		conn = nil
	}

	return conn.ID
}

func (c *Controller) Remove(conn *protocol.Connection, infoID string, connType protocol.ConnectionType) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch connType {
	case protocol.ConnAPI:
		c.apiConn = nil

	case protocol.ConnAgent:
		delete(c.agentsConn, infoID)

	case protocol.ConnLB:
		delete(c.lbConn, infoID)
	}
}

func (c *Controller) createNewMessageNode() (*protocol.AgentInfo, *protocol.LBalancerInfo, error) {
	msgNodeID := crypto.GenerateID(crypto.MESSAGE_NODE)

	agent, err := c.createNewAgent(c.BASE_PORT_AGENT, msgNodeID)
	if err != nil {
		return nil, nil, err
	}

	//TODO: handle cleaning up agent (no point for agent that doesnt have lb)
	lb, err := c.createNewLoadBalancer(c.BASE_PORT_LBALANCER)
	if err != nil {
		return nil, nil, err
	}

	agent.NodeID = msgNodeID
	lb.NodeID = msgNodeID

	log["console"].Debug("Succesfully created message node (agent: %v, lb: %v)", agent, lb)

	//only returns if both of them succeded
	return agent, lb, nil
}

func (c *Controller) ReceiveHeartBeat(msg protocol.Message, conn *protocol.Connection) {
	//log["console"].Debug("Received heart-beat: %v", msg)
	switch msg.Content {
	case "AGENT":
		c.updateAgentHeartbeat(conn.ID)
	case "LB":
		c.updateLBHeartbeat(conn.ID)
	}
}

func (c *Controller) AsyncEvent(msg protocol.Message, conn *protocol.Connection) {
	log["console"].Debug("called AsyncEvent: %v", msg)

	var response protocol.Message
	switch msg.Type {
	case protocol.CREATE:
		response = c.handleCreate(msg)
	case protocol.RECOVER:
		lb := c.LbInfo[conn.ID]
		msJson, err := json.ToJSON(lb.Microservices)
		if err != nil {
			panic(err)
		}

		response = protocol.Message{
			ID:      msg.ID,
			Type:    msg.Type,
			Code:    protocol.SUCCESS,
			Content: msJson,
		}

	default:
		errStr := fmt.Sprintf("Unsupported NodeAsyncEvent type: %s", msg.Type)
		log["console"].Error(errStr)
		response = protocol.Message{
			ID:      msg.ID,
			Type:    msg.Type,
			Code:    protocol.ERROR,
			Content: errStr,
		}
	}

	if err := protocol.Send(conn.RW.Writer, response); err != nil {
		log["console"].Error("Send response in node async event: %v", err)
	}

}

func (c *Controller) handleCreate(msg protocol.Message) protocol.Message {
	//first check if for some reason api doesnt know lb with this ms exists
	var lb *protocol.LBalancerInfo = nil
	mServices, ok := c.Microservices[protocol.ServiceType(msg.Content)]

	//TODO: take first one, later do load
	if ok {
		log["console"].Debug("Found services that api though didnt existed")
		ms := mServices[0]
		for _, l := range c.LbInfo {
			if l.NodeID == ms.NodeID {
				lb = l
				break
			}
		}

		lbJson, err := json.ToJSON(lb)
		code := protocol.SUCCESS
		if err != nil {
			lbJson = err.Error()
			code = protocol.ERROR
		}

		return protocol.Message{
			ID:           msg.ID,
			SessionID:    msg.SessionID,
			ConnectionID: msg.ConnectionID,
			Type:         protocol.UPDATE,
			Code:         code,
			Content:      lbJson,
		}
	}

	// api gateway asks controller to create new service based on user request
	_, lb, err := c.createNewService(protocol.ServiceType(msg.Content))

	if err != nil {
		log["console"].Error("Error creating service: %v", err)
		return protocol.Message{
			ID:           msg.ID,
			SessionID:    msg.SessionID,
			ConnectionID: msg.ConnectionID,
			Type:         protocol.CREATE,
			Code:         protocol.ERROR,
			Content:      err.Error(),
		}
	} else {

		lbJson, err := json.ToJSON(lb)
		code := protocol.SUCCESS
		if err != nil {
			lbJson = err.Error()
			code = protocol.ERROR
		}

		//update api lb data
		return protocol.Message{
			ID:           msg.ID,
			SessionID:    msg.SessionID,
			ConnectionID: msg.ConnectionID,
			Type:         protocol.UPDATE,
			Code:         code,
			Content:      lbJson,
		}
	}

}

func (c *Controller) watchHealth(connID string) {

	//! DO NOT SET DELAY LOWER THEN 5 sec - infinite unhealthy cykle
	allowedDelay := 6 * time.Second

	if lb, ok := c.LbInfo[connID]; ok {
		for {
			timestamp := lb.LastHeartbeat
			if time.Since(timestamp) > allowedDelay {
				lb.Status = protocol.Unhealthy
				log["console"].Debug("Lb %v has status UNHEALTHY", lb.ID)

				go c.restartLb(lb)
				return
			}

			time.Sleep(1 * time.Second)
		}
	} else if agent, ok := c.AgentInfo[connID]; ok {
		for {
			timestamp := agent.LastHeartbeat
			if time.Since(timestamp) > allowedDelay {
				agent.Status = protocol.Unhealthy
				log["console"].Debug("agent %v has status UNHEALTHY", agent.ID)

				go c.restartAgent(agent)
				return
			}

			time.Sleep(1 * time.Second)
		}
	} else {
		log["console"].Error("lb nor agent found with connection id: %v", connID)
	}
}

func (c *Controller) recoverConnections(conn *protocol.Connection) error {

	var err error
	recoverMsg := protocol.Message{
		ID:   crypto.GenerateID(crypto.MESSAGE_ID),
		Type: protocol.RECOVER,
	}

	if agent, ok := c.AgentInfo[conn.ID]; ok {

		recoverMsg.Content, err = json.EncodeContentStr(agent.Microservices)
		if err != nil {
			return logger.StrToError(log, "Encoding content: %v", err)
		}

		resp, err := conn.SendRequest(recoverMsg)
		if err != nil {
			return logger.StrToError(log, "Sending recover request: %v", err)
		} else if resp.Code != protocol.SUCCESS {
			return logger.StrToError(log, "Sending recover request: %v", resp)
		}

	} else if lb, ok := c.LbInfo[conn.ID]; ok {

		recoverMsg.Content, err = json.EncodeContentStr(lb.Microservices)
		if err != nil {
			return logger.StrToError(log, "Encoding content: %v", err)
		}

		resp, err := conn.SendRequest(recoverMsg)
		if err != nil {
			return logger.StrToError(log, "Sending recover request: %v", err)
		} else if resp.Code != protocol.SUCCESS {
			return logger.StrToError(log, "Sending recover request: %v", resp)
		}

	} else {
		return logger.StrToError(log, "agent and loadbalancer not found in connection %v", conn)
	}

	return nil
}

// TODO!: if cache file is getting corrupted try saving to tmp file then to cache file (keep both files)
func (c *Controller) cacheStateToFile() {
	for {
		time.Sleep(5 * time.Second)

		file, err := os.OpenFile(CACHE_FILE, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0664)
		if err != nil {
			log["console"].Error("Opening file: %v", err)
			return
		}
		defer file.Close()

		controllerJson, err := json.ToJSON(c)
		if err != nil {
			log["console"].Error("Converting to JSON: %v", err)
			return
		}

		size, err := file.Write([]byte(controllerJson))
		if err != nil {
			log["console"].Error("Writing to file: %v", err)
			return
		}

		//force to flush contents to file
		if err := file.Sync(); err != nil {
			log["console"].Error("Sync file: %v", err)
			return
		}
		file.Close()

		log["console"].Debug("Succesfully written %v to file %v", size, CACHE_FILE)

	}
}

func (c *Controller) recoverStateFromFile() error {
	//recover data from file
	fileData, err := os.ReadFile(CACHE_FILE)
	if err != nil {
		return err
	}

	dataJson := string(fileData)
	prevState, err := json.FromJSON[*Controller](dataJson)
	if err != nil {
		return err
	}

	c.BASE_PORT_AGENT = prevState.BASE_PORT_AGENT
	c.BASE_PORT_LBALANCER = prevState.BASE_PORT_LBALANCER
	c.HOST_AGENT = prevState.HOST_AGENT
	c.HOST_LBALANCER = prevState.HOST_LBALANCER
	c.NextID = prevState.NextID
	c.PORT = prevState.PORT
	c.Microservices = prevState.Microservices

	//old maps are invalid but nodeInfo is, store old data and make new maps
	lbInfo := prevState.LbInfo
	agentInfo := prevState.AgentInfo

	c.LbInfo = make(map[string]*protocol.LBalancerInfo)
	c.lbConn = make(map[string]*protocol.Connection)

	c.AgentInfo = make(map[string]*protocol.AgentInfo)
	c.agentsConn = make(map[string]*protocol.Connection)

	log["console"].Info("Recovered state from file.")
	log["console"].Debug("Services: %v\n, Agents: %v\n, Lbs: %v", c.Microservices, agentInfo, lbInfo)

	//TODO: maybe later add waitgroup to wait for rutines to finish, but take api in equation
	var wg sync.WaitGroup

	//recover state from data
	//this will create new maps
	go func() {
		defer wg.Done()
		for _, agent := range agentInfo {
			if err := c.connectToAgent(agent); err != nil {
				log["console"].Warning("Cant connect to agent when recovering: %v", err)
				log["console"].Info("Restarting agent %v!", agent)

				wg.Add(1)

				go c.restartAgent(agent)
			} else {
				log["console"].Debug("Connected to agent")
			}
		}
	}()

	go func() {
		defer wg.Done()

		for _, lb := range lbInfo {
			if err := c.connectToLB(lb); err != nil {
				log["console"].Warning("Cant connect to lb when recovering: %v", err)
				log["console"].Info("Restarting lb %v!", lb)

				wg.Add(1)

				go c.restartLb(lb)
			} else {
				log["console"].Debug("Connected to lb")
			}
		}
	}()

	wg.Wait()

	//connection to api isnt renewed because api will try to connect to controller by itself
	if err = c.run(); err != nil {
		return err
	}

	//! this is the reason why controller starts again
	if err = c.syncWithAPI(); err != nil {
		return err
	}

	return nil
}

func (c *Controller) HandShake(conn *protocol.Connection) {
}

func (c *Controller) String() string {
	return "CONTROLLER"
}

// ################################## CORE FUNCTIONS #####################################

// ################################# API FUNCTIONS ################################
func (c *Controller) acceptConnection() {
	for {
		nc, err := c.listener.Accept()
		if err != nil {
			log["console"].Error("Connection accept error: %v", err)
			continue
		}

		log["console"].Info("Accepted new connection")

		go c.handleConnection(nc)
	}
}

func (c *Controller) handleConnection(nc net.Conn) {
	conn := protocol.NewConnection(nc)

	msg, err := protocol.Receive(conn.RW.Reader)
	if err != nil {
		log["console"].Error("connection register error: %v", err)
		return
	}

	log["console"].Debug("Message from api: %v", msg)

	//to controller for now only api will connect
	var response protocol.Message

	if msg.Type != protocol.HELLO {
		errMsg := "expected Handshake with type HELLO"
		log["console"].Error(errMsg)
		response = protocol.Message{
			ID:        msg.ID,
			SessionID: msg.SessionID,
			Code:      protocol.ERROR,
			Type:      msg.Type,
			Content:   errMsg,
		}

		goto send
	}

	if msg.Content == "API" {

		response = protocol.Message{
			ID:      msg.ID,
			Type:    msg.Type,
			Code:    protocol.SUCCESS,
			Content: c.String(),
		}

		c.Register(conn, protocol.ConnAPI, "")
		log["console"].Info("Connected to api: %s", conn.ID)

		go conn.ReceiveLoop(c)

	} else {
		errMsg := fmt.Sprintf("Unknow Node connected %v", msg)
		log["console"].Error(errMsg)
		response = protocol.Message{
			ID:        msg.ID,
			SessionID: msg.SessionID,
			Code:      protocol.ERROR,
			Type:      msg.Type,
			Content:   errMsg,
		}
	}

send:

	log["console"].Debug("Sending response to api %v", response)
	err = protocol.Send(conn.RW.Writer, response)
	if err != nil {
		log["console"].Error("Error sending response to api: %v", err)
	}
}

// this func will be called when loadbalancers and microservices change without api knowing it
func (c *Controller) syncWithAPI() error {
	var lbs []*protocol.LBalancerInfo
	for _, lb := range c.LbInfo {
		if lb.Status == protocol.Healthy {
			lbs = append(lbs, lb)
		}
	}

	newData, err := json.ToJSON(lbs)
	if err != nil {
		return err
	}

	msg := protocol.Message{
		ID:      crypto.GenerateID(crypto.MESSAGE_ID),
		Type:    protocol.UPDATE,
		Content: newData,
	}

	//wait for api conn to be established
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)

		if c.apiConn != nil {
			log["console"].Info("Api conn established!")
			break
		}

		log["console"].Debug("Api conn not established yet...")
	}

	if c.apiConn == nil {
		return logger.StrToError(log, "Failed to establish api conn")
	}

	resp, err := c.apiConn.SendRequest(msg)
	if err != nil {
		return logger.StrToError(log, "Sync api send request: %v", err)
	} else if !resp.IsCorrect(msg.Type) {
		return logger.StrToError(log, "Sync api send request: %v", resp)
	}

	return nil
}

// ################################# API FUNCTIONS ################################

// ################################# AGENT FUNCTIONS ###############################
func (c *Controller) GetNextNodeID() uint32 {
	return atomic.AddUint32(&c.NextID, 1)
}

// createNewAgent spawns an agent process, assigns host:port, and registers it.
// Agent binary must accept --host and --port flags (e.g. ./agent --host localhost --port 10001).
func (c *Controller) createNewAgent(port string, nodeID string) (*protocol.AgentInfo, error) {
	id := fmt.Sprintf("%d", c.GetNextNodeID())

	cmd := exec.Command(
		platform.Executable("agent"),
		"--port", port,
		"--nodeID", nodeID,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, logger.StrToError(log, "start agent process: %v\n", err)
	}

	log["console"].Info("Started agent process: %d", cmd.Process.Pid)

	//TODO: later change hardcoded host, and port will be sent by agent
	agent := &protocol.AgentInfo{
		ID:            id,
		Host:          "localhost",
		Port:          port,
		Process:       cmd.Process,
		Status:        protocol.Healthy,
		Microservices: make(map[protocol.ServiceType][]*protocol.MsInfo),
	}

	err := c.connectToAgent(agent)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, logger.StrToError(log, "connect to agent: %v\n", err)
	}

	log["console"].Debug("Agent started {ID:%s PID:%d}\n", id, cmd.Process.Pid)
	return agent, nil
}

func (c *Controller) connectToAgent(agent *protocol.AgentInfo) error {
	address := net.JoinHostPort(agent.Host, agent.Port)

	conn, err := protocol.TryToConnect(address, 3*time.Second)
	if err != nil {
		return logger.StrToError(log, "timeout connecting to agent: %v", err)
	}

	conn.Mu.Lock()
	c.agentsConn[agent.ID] = conn
	c.AgentInfo[conn.ID] = agent
	conn.Mu.Unlock()

	agent.LastHeartbeat = time.Now()

	go conn.ReceiveLoop(c)
	go c.watchHealth(conn.ID)

	//handshake
	handshakeResp, err := conn.SendRequest(protocol.Message{
		ID:      crypto.GenerateID(crypto.MESSAGE_ID),
		Type:    protocol.HELLO,
		Content: c.String(),
	})
	if err != nil {
		return logger.StrToError(log, "Sending handshake to agent %v", err)
	} else if handshakeResp.Code != protocol.SUCCESS || handshakeResp.Type != protocol.HELLO || handshakeResp.Content != "AGENT" {
		return logger.StrToError(log, "Response from agent %v", handshakeResp)
	}

	return nil
}

// ? might make this a single function that takes 1 param and depending on that param switches between all manager containers
// ? param: (type ClientType) return (Conn map)
func (c *Controller) GetAgents() []*protocol.Connection {
	// cm.mu.Lock()
	// defer cm.mu.Unlock()
	out := make([]*protocol.Connection, 0, len(c.agentsConn))
	for _, a := range c.agentsConn {
		out = append(out, a)
	}

	return out
}

// ? same as 'GetAgents' make 1 func and return based on param
func (c *Controller) GetAgentConnByID(id string) *protocol.Connection {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.agentsConn[id]
}

func (c *Controller) GetAgentInfoByID(id string) *protocol.AgentInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, a := range c.AgentInfo {
		if a.ID == id {
			return a
		}
	}

	panic("Agent info with id: " + id + " notfound")
}

func (c *Controller) KillAllAgents() {
	for _, a := range c.AgentInfo {
		if a == nil || a.Process == nil {
			continue
		}
		a.Process.Kill()

		conn := c.agentsConn[a.ID]
		conn.Close()
	}

	// reset maps
	c.AgentInfo = make(map[string]*protocol.AgentInfo)
	c.agentsConn = make(map[string]*protocol.Connection)
}

func (c *Controller) updateAgentHeartbeat(agentConnID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if agent, ok := c.AgentInfo[agentConnID]; ok {
		agent.Mu.Lock()
		agent.LastHeartbeat = time.Now()
		agent.Status = protocol.Healthy
		agent.Mu.Unlock()
		log["console"].Debug("Agent %v heartbeat is %v", agent.ID, agent.LastHeartbeat)

	} else {
		log["console"].Error("Unknown agent in connection with id: %v", agentConnID)
	}
}

func (c *Controller) restartAgent(agent *protocol.AgentInfo) {

	var prevConn *protocol.Connection
	var ok bool
	if agent.Process == nil {
		log["console"].Debug("agent: %v process nil", agent.ID)
		goto newAgent
	}
	//make sure agent process is killed
	if err := agent.Process.Kill(); err != nil {
		log["console"].Warning("killing agent %v process: %v", agent.ID, err)
	}

	prevConn, ok = c.agentsConn[agent.ID]
	if !ok {
		log["console"].Debug("agent: %v previous connection nil", agent.ID)
		goto newAgent
	}

	delete(c.AgentInfo, prevConn.ID)

	if err := prevConn.Nc.Close(); err != nil {
		log["console"].Warning("close previous agent connection: %v", err)
	}

newAgent:
	cmd := exec.Command(
		platform.Executable("agent"),
		"--port", agent.Port,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log["console"].Error("start agent process: %v", err)
		return
	}

	agent.Process = cmd.Process

	log["console"].Info("Restarted agent process: %d", agent.Process.Pid)

	err := c.connectToAgent(agent)
	if err != nil {
		log["console"].Error("connect to agent: %v", err)
		return
	}

	agentConn := c.agentsConn[agent.ID]
	err = c.recoverConnections(agentConn)
	if err != nil {
		log["console"].Error("recover agent connections: %v", err)
		return
	}

	log["console"].Debug("Succesfully restarted agent %v", agent.ID)
}

// ################################# AGENT FUNCTIONS ###################################

// ################################ LOAD BALANCER FUNCTIONS ###################################
func (c *Controller) createNewLoadBalancer(port string) (*protocol.LBalancerInfo, error) {
	id := fmt.Sprintf("%d", c.GetNextNodeID())

	cmd := exec.Command(
		platform.Executable("lb"),
		"--port", port,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, logger.StrToError(log, "start loadbalancer process: %v\n", err)
	}

	log["console"].Info("Started lb process: %d", cmd.Process.Pid)

	//TODO: later change hardcoded host, and port will be sent by lb
	lb := &protocol.LBalancerInfo{
		ID:            id,
		Host:          "localhost",
		Port:          port,
		Process:       cmd.Process,
		Status:        protocol.Healthy,
		Microservices: make(map[protocol.ServiceType][]*protocol.MsInfo),
	}

	err := c.connectToLB(lb)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, logger.StrToError(log, "connect to loadbalancer: %v\n", err)
	}

	log["console"].Debug("Loadbalancer started {ID:%s PID:%d}\n", id, cmd.Process.Pid)
	return lb, nil
}

func (c *Controller) connectToLB(lb *protocol.LBalancerInfo) error {
	address := net.JoinHostPort(lb.Host, lb.Port)

	conn, err := protocol.TryToConnect(address, 3*time.Second)
	if err != nil {
		return logger.StrToError(log, "timeout connecting to loadbalancer: %v", err)
	}

	conn.Mu.Lock()
	c.lbConn[lb.ID] = conn
	c.LbInfo[conn.ID] = lb
	conn.Mu.Unlock()

	//c.Register(conn, protocol.ConnLB, lb.ID)
	lb.LastHeartbeat = time.Now()

	go conn.ReceiveLoop(c)
	go c.watchHealth(conn.ID)

	//handshake
	handshakeResp, err := conn.SendRequest(protocol.Message{
		ID:      crypto.GenerateID(crypto.MESSAGE_ID),
		Type:    protocol.HELLO,
		Content: c.String(),
	})
	if err != nil {
		return logger.StrToError(log, "Sending handshake to loadbalancer %v", err)
	} else if handshakeResp.Code != protocol.SUCCESS || handshakeResp.Type != protocol.HELLO || handshakeResp.Content != "LOADBALANCER" {
		return logger.StrToError(log, "Response from loadbalancer %v", handshakeResp)
	}

	return nil
}

func (c *Controller) KillAllLoadBalancers() {
	for _, lb := range c.LbInfo {
		if lb == nil || lb.Process == nil {
			continue
		}
		lb.Process.Kill()

		conn := c.lbConn[lb.ID]
		conn.Close()
	}

	// reset maps
	c.LbInfo = make(map[string]*protocol.LBalancerInfo)
	c.lbConn = make(map[string]*protocol.Connection)
}

func (c *Controller) updateLBHeartbeat(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if lb, ok := c.LbInfo[id]; ok {
		lb.Mu.Lock()
		lb.LastHeartbeat = time.Now()
		lb.Status = protocol.Healthy
		lb.Mu.Unlock()
		log["console"].Debug("LoadBalancer %v heartbeat is %v", lb.ID, lb.LastHeartbeat)

	} else {
		log["console"].Error("Unknown lb in connection: %v", c.agentsConn[lb.ID])
	}
}

func (c *Controller) restartLb(lb *protocol.LBalancerInfo) {

	var prevConn *protocol.Connection
	if lb.Process == nil {
		goto noProcess
	}

	//make sure lb process is killed
	if err := lb.Process.Kill(); err != nil {
		log["console"].Warning("killing lb %v process: %v", lb.ID, err)
	}

	prevConn = c.lbConn[lb.ID]

	if err := prevConn.Nc.Close(); err != nil {
		log["console"].Warning("close previous lb connection: %v", err)
	}

noProcess:
	cmd := exec.Command(
		platform.Executable("lb"),
		"--port", lb.Port,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log["console"].Error("start lb process: %v", err)
		return
	}

	lb.Process = cmd.Process

	log["console"].Info("Restarted lb process: %d", lb.Process.Pid)

	err := c.connectToLB(lb)
	if err != nil {
		log["console"].Error("connect to lb: %v", err)
		return
	}

	lbConn := c.lbConn[lb.ID]

	err = c.recoverConnections(lbConn)
	if err != nil {
		log["console"].Error("recover lb connections: %v", err)
		return
	}

	log["console"].Debug("Succesfully restarted lb %v", lb.ID)
}

// ################################ LOAD BALANCER FUNCTIONS ###################################

// ################################ MICROSERVICE FUNCTIONS ###################################
func (c *Controller) createNewService(serviceType protocol.ServiceType) (*protocol.MsInfo, *protocol.LBalancerInfo, error) {

	// Ensure at least one agent exists
	c.mu.RLock()
	hasAgent := len(c.AgentInfo) > 0
	hasLB := len(c.LbInfo) > 0
	c.mu.RUnlock()

	var agent *protocol.AgentInfo = nil
	var lb *protocol.LBalancerInfo = nil
	var err error

	if !hasAgent || !hasLB {
		//TODO: Message node now doesnt have to have 1 agent and 1 lb, it will contain many lbs and 1 agent
		agent, lb, err = c.createNewMessageNode()
		if err != nil {
			return nil, nil, err
		}

	} else {
		log["console"].Debug("Message node is present. agent size: %v,  lb size: %v", len(c.AgentInfo), len(c.LbInfo))
	}

	// TODO: choose agent by load (least busy, etc.)
	/*
	*TODO: if agent/lb is found unhealthy retry 1/2 times then return error
	*(maybe use goto to start of method or handle it call higher)
	 */
	c.mu.RLock()
	if hasAgent {
		for _, a := range c.AgentInfo {
			if a.Status == protocol.Healthy {
				agent = a
				break
			} else {
				return nil, nil, logger.StrToError(log, "Found matching agent but is Unhealthy")
			}
		}
	}

	if hasLB {
		for _, l := range c.LbInfo {
			if l.Status == protocol.Healthy {
				lb = l
				break
			} else {
				return nil, nil, logger.StrToError(log, "Found matching lb but is Unhealthy")
			}
		}
	}
	c.mu.RUnlock()

	if agent == nil || lb == nil {
		return nil, nil, logger.StrToError(log, "one of nodes unavailable, one of them is nil: (agent: %v, lb: %v)\n", agent, lb)
	} else if agent.Status == protocol.Unhealthy || lb.Status == protocol.Unhealthy {
		return nil, nil, logger.StrToError(log, "one of nodes are unhealthy (agent %v, lb: %v)", agent.Status, lb.Status)
	}

	// mapping agent and lb info to agent conn
	agentConn := c.agentsConn[agent.ID]
	lbConn := c.lbConn[lb.ID]

	if agentConn == nil || lbConn == nil {
		return nil, nil, logger.StrToError(log, "One of nodes connection is nil (agent: %v, lb: %v)\n", agentConn, lbConn)
	}

	// message agent to create service
	request := protocol.Message{
		SessionID: agent.ID,
		Type:      protocol.CREATE,
		Content:   string(serviceType),
	}

	response, err := agentConn.SendRequest(request)
	if err != nil {
		return nil, nil, logger.StrToError(log, "send CREATE: %v\n", err)
	}

	//update agent health based on successful response
	c.updateAgentHeartbeat(agentConn.ID)

	ms, err := json.FromJSON[*protocol.MsInfo](response.Content)
	if err != nil {
		log["console"].Error("Parse ms from response %v", err)
	}

	//inform load balancer about newly created service
	request = protocol.Message{
		SessionID: response.SessionID,
		Type:      protocol.UPDATE,
		Content:   string(ms.ID + ";" + ms.Host + ";" + ms.Port + ";" + ms.NodeID + ";" + string(ms.Type)),
	}

	response, err = lbConn.SendRequest(request)
	if err != nil {
		return nil, nil, logger.StrToError(log, "send ms info to lb: %v\n", err)
	} else if response.Code != protocol.SUCCESS {
		return nil, nil, logger.StrToError(log, "Bad code type %d, message: %s\n", response.Code, response.Content)
	}

	//same as with agent
	c.updateLBHeartbeat(lbConn.ID)

	log["console"].Debug("LB response after getting ms data: %v", response)

	// update nodes with new microservice
	c.mu.Lock()
	c.Microservices[serviceType] = append(c.Microservices[serviceType], ms)
	c.LbInfo[lbConn.ID].Microservices[serviceType] = append(c.LbInfo[lbConn.ID].Microservices[serviceType], ms)
	c.AgentInfo[agentConn.ID].Microservices[ms.Type] = append(agent.Microservices[ms.Type], ms)
	c.mu.Unlock()

	return ms, lb, nil
}

// ################################ MICROSERVICE FUNCTIONS ###################################
