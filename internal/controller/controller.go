package controller

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
	"sync/atomic"
	"time"
)

/**

*! requests to load balancer should be in queue channel like in api gateway

 */

var log logger.Loggers = logger.NewLoggers(
	logger.WithConsole(os.Stdout, os.Stderr),
	logger.WithBaseOptions(
		logger.PrefixField("controller"),
		logger.FormatField(logger.BASE_PREFIX),
	),
)

const (
	REQUEST_PORT = ":8888"
	SYSTEM_PORT  = ":9000"
)

const (
	BASE_PORT_AGENT     string = "10000"
	BASE_PORT_LBALANCER string = "13000"

	HeartbeatTimeout = 3 * time.Second
)

// ##################################### STRUCTURES #####################################
// TODO: controller should update "lastHeartbeat" of nodes when they send hearthbeat and with every interaction
// TODO: only add second connection and use it when working with files
// TODO: if agent is in the same host as controller he can choose ports otherwise controller sets boundry or just a free port and agents sends back wich port he got in remote host
type Controller struct {
	listener net.Listener

	//? if controller will connect to more then 1 api make it same as agent storage
	apiConn *protocol.Connection

	agentsInfo map[string]*protocol.AgentInfo
	agentsConn map[string]*protocol.Connection

	lbInfo map[string]*protocol.LBalancerInfo
	lbConn map[string]*protocol.Connection

	microservices map[protocol.ServiceType][]*protocol.MsInfo

	//TODO: store list of available hosts

	mu sync.RWMutex

	nextID uint32 // id tracker of agent and load balancer
}

// ##################################### STRUCTURES #####################################

// ################################## CORE FUNCTIONS #####################################
func NewController() (*Controller, error) {
	apiListener, err := net.Listen("tcp", REQUEST_PORT)
	if err != nil {
		return nil, err
	}

	return &Controller{
		listener: apiListener,

		agentsInfo: make(map[string]*protocol.AgentInfo),
		agentsConn: make(map[string]*protocol.Connection),

		lbInfo: make(map[string]*protocol.LBalancerInfo),
		lbConn: make(map[string]*protocol.Connection),

		microservices: make(map[protocol.ServiceType][]*protocol.MsInfo),

		nextID: 0,
	}, nil
}

// starts and accepts api and system connections
func (c *Controller) Start() {
	log["console"].Info("Controller started")

	go c.acceptConnection()
}

// ? after registering connection maybe start receive loop
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

func (c *Controller) createNewMessageNode(agentPort string, lbPort string) (*protocol.AgentInfo, *protocol.LBalancerInfo, error) {
	msgNodeID := crypto.GenerateID(crypto.MESSAGE_NODE)

	agent, err := c.createNewAgent(agentPort, msgNodeID)
	if err != nil {
		return nil, nil, err
	}

	//TODO: handle cleaning up agent (no point for agent that doesnt have lb)
	lb, err := c.createNewLoadBalancer(lbPort)
	if err != nil {
		return nil, nil, err
	}

	agent.NodeID = msgNodeID
	lb.NodeID = msgNodeID

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
	mServices, ok := c.microservices[protocol.ServiceType(msg.Content.(string))]

	//TODO: take first one, later do load
	if ok {
		log["console"].Debug("Found services that api though didnt existed")
		ms := mServices[0]
		for _, l := range c.lbInfo {
			if l.NodeID == ms.NodeID {
				lb = l
				break
			}
		}

		return protocol.Message{
			ID:           msg.ID,
			SessionID:    msg.SessionID,
			ConnectionID: msg.ConnectionID,
			Type:         protocol.UPDATE,
			Content:      lb,
		}
	}

	// api gateway asks controller to create new service based on user request
	_, lb, err := c.createNewService(protocol.ServiceType(msg.Content.(string)))

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

		//update api lb data
		return protocol.Message{
			ID:           msg.ID,
			SessionID:    msg.SessionID,
			ConnectionID: msg.ConnectionID,
			Type:         protocol.UPDATE,
			Content:      lb,
		}
	}

}

func (c *Controller) watchHealth(connID string) {

	//! DO NOT SET DELAY LOWER THEN 5 sec - infinite unhealthy cykle
	allowedDelay := 6 * time.Second

	if lb, ok := c.lbInfo[connID]; ok {
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
	} else if agent, ok := c.agentsInfo[connID]; ok {
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

	if agent, ok := c.agentsInfo[conn.ID]; ok {

		recoverMsg.Content, err = json.EncodeContentStr(agent.Microservices)
		if err != nil {
			return logger.StrToErrorNew(log, "Encoding content: %v", err)
		}

		resp, err := conn.SendRequest(recoverMsg)
		if err != nil {
			return logger.StrToErrorNew(log, "Sending recover request: %v", err)
		} else if resp.Code != protocol.SUCCESS {
			return logger.StrToErrorNew(log, "Sending recover request: %v", resp)
		}

	} else if lb, ok := c.lbInfo[conn.ID]; ok {

		recoverMsg.Content, err = json.EncodeContentStr(lb.Microservices)
		if err != nil {
			return logger.StrToErrorNew(log, "Encoding content: %v", err)
		}

		resp, err := conn.SendRequest(recoverMsg)
		if err != nil {
			return logger.StrToErrorNew(log, "Sending recover request: %v", err)
		} else if resp.Code != protocol.SUCCESS {
			return logger.StrToErrorNew(log, "Sending recover request: %v", resp)
		}

	} else {
		return logger.StrToErrorNew(log, "agent and loadbalancer not found in connection %v", conn)
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
	log["console"].Info("Listening for connections on %s", c.listener.Addr())

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
	lbList := make([]*protocol.
		LBalancerInfo, 0, len(c.lbInfo))

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
		//response after receiving hello
		err = protocol.Send(conn.RW.Writer, protocol.Message{
			ID:       msg.ID,
			Type:     msg.Type,
			Code:     protocol.SUCCESS,
			IsStream: true,
			Content:  c.String(),
		})
		if err != nil {
			log["console"].Error("Send response handshake: %v", err)
		}

		log["console"].Debug("Send api response handshake succesfully")

		c.Register(conn, protocol.ConnAPI, "")
		log["console"].Info("Connected: %s", conn.ID)

		go conn.ReceiveLoop(c)

		// create for the first time (both data structures are empty)
		if len(c.lbInfo) == 0 && len(c.agentsInfo) == 0 {
			// connection with api was correct so now create message node so api can connect to it
			c.createNewMessageNode(BASE_PORT_AGENT, BASE_PORT_LBALANCER)
		}

		// convert map to list because api has to map data by itself
		for _, lb := range c.lbInfo {
			lbList = append(lbList, lb)
		}

		response = protocol.Message{
			ID:      msg.ID,
			Code:    protocol.SUCCESS,
			Type:    msg.Type,
			Content: lbList,
		}
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
func (c *Controller) syncWithAPI(conn *protocol.Connection, newData any) error {
	msg := protocol.Message{
		ID:      crypto.GenerateID(crypto.MESSAGE_ID),
		Type:    protocol.UPDATE,
		Content: newData,
	}

	if err := protocol.Send(conn.RW.Writer, msg); err != nil {
		return logger.StrToErrorNew(log, "send request: %v", err)
	}

	return nil
}

// ################################# API FUNCTIONS ################################

// ################################# AGENT FUNCTIONS ###############################
func (c *Controller) GetNextNodeID() uint32 {
	return atomic.AddUint32(&c.nextID, 1)
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
		return nil, logger.StrToErrorNew(log, "start agent process: %v\n", err)
	}

	log["console"].Info("Started agent process: %d", cmd.Process.Pid)

	//TODO: later change hardcoded host, and port will be sent by agent
	agent := &protocol.AgentInfo{
		ID:            id,
		Host:          "localhost",
		Port:          port,
		Process:       cmd.Process,
		Microservices: make(map[protocol.ServiceType][]*protocol.MsInfo),
	}

	err := c.connectToAgent(agent)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, logger.StrToErrorNew(log, "connect to agent: %v\n", err)
	}

	log["console"].Debug("Agent started {ID:%s PID:%d}\n", id, cmd.Process.Pid)
	return agent, nil
}

func (c *Controller) connectToAgent(agent *protocol.AgentInfo) error {
	address := net.JoinHostPort(agent.Host, agent.Port)

	conn := protocol.TryToConnect(address)
	if conn == nil {
		return logger.StrToErrorNew(log, "timeout connecting to agent\n")
	}

	conn.Mu.Lock()
	c.agentsConn[agent.ID] = conn
	c.agentsInfo[conn.ID] = agent
	conn.Mu.Unlock()

	//c.Register(conn, protocol.ConnAgent, agent.ID)
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
		return logger.StrToErrorNew(log, "Sending handshake to agent %v", err)
	} else if handshakeResp.Code != protocol.SUCCESS || handshakeResp.Type != protocol.HELLO || handshakeResp.Content != "AGENT" {
		return logger.StrToErrorNew(log, "Response from agent %v", handshakeResp)
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
	for _, a := range c.agentsInfo {
		if a.ID == id {
			return a
		}
	}

	panic("Agent info with id: " + id + " notfound")
}

func (c *Controller) KillAllAgents() {
	for _, a := range c.agentsInfo {
		if a == nil || a.Process == nil {
			continue
		}
		a.Process.Kill()
	}

	// reset maps
	c.agentsInfo = make(map[string]*protocol.AgentInfo)
	c.agentsConn = make(map[string]*protocol.Connection)
}

func (c *Controller) updateAgentHeartbeat(agentConnID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if agent, ok := c.agentsInfo[agentConnID]; ok {
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

	//make sure agent process is killed
	if err := agent.Process.Kill(); err != nil {
		log["console"].Error("killing agent %v process: %v", agent.ID, err)
		return
	}

	prevConn := c.agentsConn[agent.ID]
	//defer c.Remove(prevConn, agent.ID, protocol.ConnAgent)

	if err := prevConn.Nc.Close(); err != nil {
		log["console"].Error("close previous agent connection: %v", err)
		return
	}

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
		return nil, logger.StrToErrorNew(log, "start loadbalancer process: %v\n", err)
	}

	log["console"].Info("Started lb process: %d", cmd.Process.Pid)

	//TODO: later change hardcoded host, and port will be sent by lb
	lb := &protocol.LBalancerInfo{
		ID:            id,
		Host:          "localhost",
		Port:          port,
		Process:       cmd.Process,
		Microservices: make(map[protocol.ServiceType][]*protocol.MsInfo),
	}

	err := c.connectToLB(lb)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, logger.StrToErrorNew(log, "connect to loadbalancer: %v\n", err)
	}

	log["console"].Debug("Loadbalancer started {ID:%s PID:%d}\n", id, cmd.Process.Pid)
	return lb, nil
}

func (c *Controller) connectToLB(lb *protocol.LBalancerInfo) error {
	address := net.JoinHostPort(lb.Host, lb.Port)

	conn := protocol.TryToConnect(address)
	if conn == nil {
		return logger.StrToErrorNew(log, "timeout connecting to loadbalancer\n")
	}

	conn.Mu.Lock()
	c.lbConn[lb.ID] = conn
	c.lbInfo[conn.ID] = lb
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
		return logger.StrToErrorNew(log, "Sending handshake to loadbalancer %v", err)
	} else if handshakeResp.Code != protocol.SUCCESS || handshakeResp.Type != protocol.HELLO || handshakeResp.Content != "LOADBALANCER" {
		return logger.StrToErrorNew(log, "Response from loadbalancer %v", handshakeResp)
	}

	return nil
}

func (c *Controller) KillAllLoadBalancers() {
	for _, lb := range c.lbInfo {
		if lb == nil || lb.Process == nil {
			continue
		}
		lb.Process.Kill()
	}

	// reset maps
	c.lbInfo = make(map[string]*protocol.LBalancerInfo)
	c.lbConn = make(map[string]*protocol.Connection)
}

func (c *Controller) updateLBHeartbeat(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if lb, ok := c.lbInfo[id]; ok {
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
	//make sure lb process is killed

	if err := lb.Process.Kill(); err != nil {
		log["console"].Error("killing lb %v process: %v", lb.ID, err)
		return
	}

	prevConn := c.lbConn[lb.ID]
	//defer c.Remove(prevConn, lb.ID, protocol.ConnLB)

	if err := prevConn.Nc.Close(); err != nil {
		log["console"].Error("close previous lb connection: %v", err)
		return
	}

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

	// if err := c.syncWithAPI(c.apiConn, []*protocol.LBalancerInfo{lb}); err != nil {
	// 	log["console"].Error("sync with api: %v", err)
	// 	return
	// }

	// log["console"].Debug("Succesfully restarted lb %v", lb.ID)
}

// ################################ LOAD BALANCER FUNCTIONS ###################################

// ################################ MICROSERVICE FUNCTIONS ###################################
func (c *Controller) createNewService(serviceType protocol.ServiceType) (*protocol.MsInfo, *protocol.LBalancerInfo, error) {

	// Ensure at least one agent exists
	c.mu.RLock()
	hasAgent := len(c.agentsInfo) > 0
	hasLB := len(c.lbInfo) > 0
	c.mu.RUnlock()

	var agent *protocol.AgentInfo
	var lb *protocol.LBalancerInfo
	var err error

	if !hasAgent || !hasLB {
		//TODO: Message node now doesnt have to have 1 agent and 1 lb, it will contain many lbs and 1 agent
		agent, lb, err = c.createNewMessageNode(BASE_PORT_AGENT, BASE_PORT_LBALANCER)
		if err != nil {
			return nil, nil, err
		}

	}

	// TODO: choose agent by load (least busy, etc.)
	c.mu.RLock()
	if hasAgent {
		for _, a := range c.agentsInfo {
			agent = a
			break
		}
	}
	c.mu.RUnlock()

	c.mu.RLock()
	if hasLB {
		for _, l := range c.lbInfo {
			lb = l
			break
		}
	}
	c.mu.RUnlock()

	if agent == nil || lb == nil {
		return nil, nil, logger.StrToErrorNew(log, "no message node available: passed agent/lb creation and iteration and still no nodes were found (a: %v, l: %v)\n", agent, lb)
	}

	// mapping agent and lb info to agent conn
	agentConn := c.agentsConn[agent.ID]
	lbConn := c.lbConn[lb.ID]

	if agentConn == nil || lbConn == nil {
		return nil, nil, logger.StrToErrorNew(log, "One of message node connection is nil agent: %v, lb: %v\n", agentConn, lbConn)
	}

	// message agent to create service
	request := protocol.Message{
		SessionID: agent.ID,
		Type:      protocol.CREATE,
		Content:   serviceType,
	}

	response, err := agentConn.SendRequest(request)
	if err != nil {
		return nil, nil, logger.StrToErrorNew(log, "send CREATE: %v\n", err)
	}

	//update agent health based on successful response
	c.updateAgentHeartbeat(agentConn.ID)

	ms, err := json.FromJSON[*protocol.MsInfo](response.Content.(string))
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
		return nil, nil, logger.StrToErrorNew(log, "send ms info to lb: %v\n", err)
	} else if response.Code != protocol.SUCCESS {
		return nil, nil, logger.StrToErrorNew(log, "Bad code type %d, message: %s\n", response.Code, response.Content)
	}

	//same as with agent
	c.updateLBHeartbeat(lbConn.ID)

	log["console"].Debug("LB response after getting ms data: %v", response)

	// update nodes with new microservice
	c.mu.Lock()
	c.microservices[serviceType] = append(c.microservices[serviceType], ms)
	c.lbInfo[lbConn.ID].Microservices[serviceType] = append(c.lbInfo[lbConn.ID].Microservices[serviceType], ms)
	c.agentsInfo[agentConn.ID].Microservices[ms.Type] = append(agent.Microservices[ms.Type], ms)
	c.mu.Unlock()

	return ms, lb, nil
}

// ################################ MICROSERVICE FUNCTIONS ###################################
