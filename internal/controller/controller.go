package controller

import (
	"bufio"
	"fmt"
	"net"
	crypto "networking/tcp/internal/cryptography"
	"networking/tcp/internal/protocol"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

/**

*! requests to load balancer should be in queue channel like in api gateway

 */

// TODO: NOW!: make connection handler handle both connections, to controller and from controller, but make 2 instances of them, one from other to
// TODO: NOW!: cleanup code a bit
const (
	REQUEST_PORT = ":8888"
	SYSTEM_PORT  = ":9000"
)

const (
	BASE_PORT_AGENT     uint16 = 10000
	BASE_PORT_LBALANCER uint16 = 15000

	HeartbeatTimeout     = 3 * time.Second
	HeartbeatCheckPeriod = 500 * time.Millisecond
)

// ------------------------------------ STRUCTURES ----------------------------------------

// TODO: if agent is in the same host as controller he can choose ports otherwise controller sets boundry or just a free port and agents sends back wich port he got in remote host
type Controller struct {
	apiListener    net.Listener
	systemListener net.Listener

	//? if controller will connect to more then 1 api make it same as agent storage
	apiConn map[*protocol.Connection]struct{}

	agentsInfo map[string]*protocol.AgentInfo
	agentsConn map[string]*protocol.Connection

	lbInfo map[string]*protocol.LBalancerInfo
	lbConn map[string][]*protocol.Connection

	microservices map[string][]*protocol.MsInfo

	//TODO: store list of available hosts

	mu sync.RWMutex

	nextID uint32 // id tracker of agent and load balancer
}

// ------------------------------------ STRUCTURES ----------------------------------------

// ################################ CONNECTION FUNCTIONS ################################

// Node id is unused for API
func (c *Controller) Register(conn *protocol.Connection, connType protocol.ConnectionType, nodeID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	//TODO: make it that nodes generate thier id and report back to controller wich id they have
	conn.ID = crypto.GenerateID()

	switch connType {
	case protocol.ConnAPI:
		c.apiConn[conn] = struct{}{}
	case protocol.ConnAgent:
		c.agentsConn[nodeID] = conn
	case protocol.ConnLB:
		c.lbConn[nodeID] = append(c.lbConn[nodeID], conn)
	}

	return conn.ID
}

func (c *Controller) Remove(conn *protocol.Connection, connType protocol.ConnectionType) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch connType {
	case protocol.ConnAPI:
		delete(c.apiConn, conn)

	case protocol.ConnAgent:
		delete(c.agentsConn, conn.ID)

	case protocol.ConnLB:
		//TODO: fix deleting one conn from lbconn
		delete(c.lbConn, conn.ID)

	}
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
func (c *Controller) GetAgentByID(id string) *protocol.Connection {
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
		if a == nil || a.Cmd == nil || a.Cmd.Process == nil {
			return
		}
		a.Cmd.Process.Signal(syscall.SIGTERM)
	}
}

// ################################ CONNECTION FUNCTIONS ################################

// ################################## CORE FUNCTIONS #####################################
func NewController() (*Controller, error) {
	apiListener, err := net.Listen("tcp", REQUEST_PORT)
	if err != nil {
		return nil, err
	}

	systemListener, err := net.Listen("tcp", SYSTEM_PORT)
	if err != nil {
		return nil, err
	}

	return &Controller{
		apiListener:    apiListener,
		systemListener: systemListener,

		apiConn: make(map[*protocol.Connection]struct{}),

		agentsInfo: make(map[string]*protocol.AgentInfo),
		agentsConn: make(map[string]*protocol.Connection),

		lbInfo: make(map[string]*protocol.LBalancerInfo),
		lbConn: make(map[string][]*protocol.Connection),

		microservices: make(map[string][]*protocol.MsInfo),

		nextID: 0,
	}, nil
}

// starts and accepts api and system connections
func (c *Controller) Start() {
	fmt.Println("Controller started")

	fmt.Println("Almost starting api acc func")
	go c.acceptAPI()
	fmt.Println("After staring api acc func")
	go c.acceptSystem()
	//go c.runHeartbeatChecker()
}

func (c *Controller) acceptAPI() {
	fmt.Println("Started api acc func")
	fmt.Println("Listening for API on", c.apiListener.Addr())

	for {
		conn, err := c.apiListener.Accept()
		if err != nil {
			fmt.Println("API accept error:", err)
			continue
		}

		fmt.Println("Accepted new api connection")

		go c.handleAPIConn(conn)
	}
}

func (c *Controller) acceptSystem() {
	fmt.Println("Listening for System on", c.systemListener.Addr())

	for {
		conn, err := c.systemListener.Accept()
		if err != nil {
			fmt.Println("System accept error:", err)
			continue
		}

		fmt.Println("Accepted new system node connection")

		go c.handleSystemConn(conn)
	}
}

func (c *Controller) handleAPIConn(nc net.Conn) {
	conn := protocol.NewConnection(nc)
	defer conn.Close()

	msg, err := protocol.Receive(conn.RW.Reader)
	if err != nil {
		fmt.Println("api register error:", err)
		return
	}

	//gotta receive just reg_api to sync request sending and receiving
	var response protocol.Message
	if msg.Type != protocol.REG_API {
		errMsg := "expected type REGISTER API"
		fmt.Println(errMsg)
		response = protocol.Message{
			Code:    protocol.ERROR,
			Type:    msg.Type,
			Content: errMsg,
		}

		protocol.Send(conn.RW.Writer, response)
		//TODO: check if it will clean up conn if it fails
		return
	}

	c.Register(conn, protocol.ConnAPI, "")
	defer c.Remove(conn, protocol.ConnAPI)

	fmt.Println("API connected:", conn.ID)

	response = protocol.Message{
		SessionID:    msg.SessionID,
		ConnectionID: msg.ConnectionID,
		Code:         protocol.SUCCESS,
		Type:         msg.Type,
	}

	protocol.Send(conn.RW.Writer, response)

	for {
		req, err := protocol.Receive(conn.RW.Reader)
		if err != nil {
			fmt.Println("api disconected:", err)
			return
		}

		// Sequential: preserves request/response order. For concurrency, use a per-conn
		// request queue and worker that serializes responses.
		c.handleAPIRequst(conn, req)

	}

}

func (c *Controller) handleSystemConn(nc net.Conn) {
	conn := protocol.NewConnection(nc)
	defer conn.Close()

	msg, err := protocol.Receive(conn.RW.Reader)
	if err != nil {
		fmt.Println("system register error:", err)
		return
	}

	var connType protocol.ConnectionType
	switch msg.Type {
	case protocol.REG_AGENT:
		//TODO: this will be later used in register
		//port := msg.Content
		connType = protocol.ConnAgent
		//agent have to provide thier id to be registered
		id := msg.Content

		c.Register(conn, connType, id)
		defer c.Remove(conn, connType)

		//TODO: for now localhost change to assignet host from cotroller
		// c.registerAgentMetadata(conn.ID, "localhost", port)
		// defer c.unregisterAgentMetadata(conn.ID)

		fmt.Print("Agent registered ")

	case protocol.REG_LB:
		fmt.Print("LoadBalancer registered ")
	default:
		fmt.Println("Unknown system client")
		return
	}

	// if connType != protocol.ConnAPI {
	// 	c.connManager.Register(conn, connType)
	// 	defer c.connManager.Remove(conn, connType)
	// }

	fmt.Print(conn.ID)

	for {
		req, err := protocol.Receive(conn.RW.Reader)
		if err != nil {
			fmt.Println("system node disconected")
			//? handle agent disconect func, but i think it is unesecary

			return
		}

		switch connType {
		case protocol.ConnAgent:
			c.handleAgentMessage(conn, req)
		case protocol.ConnLB:
			c.handleLBMessage(conn, req)
		}

	}
}

func (c *Controller) handleAPIRequst(conn *protocol.Connection, msg protocol.Message) {
	switch msg.Type {
	case protocol.PING:

		//? later maybe change this arg to protocol.PING
		ms, err := c.findService("ping")

		if err != nil {
			resp := protocol.Message{
				Type:    protocol.PING,
				Code:    protocol.ERROR,
				Content: err.Error(),
			}

			protocol.Send(conn.RW.Writer, resp)
			return
		}

		resp := protocol.Message{
			Type:    protocol.PING,
			Code:    protocol.SUCCESS,
			Content: ms.Host + ms.Port,
		}

		protocol.Send(conn.RW.Writer, resp)

	default:

		resp := protocol.Message{
			Type:    protocol.PING,
			Code:    protocol.ERROR,
			Content: "Invalid message type",
		}

		protocol.Send(conn.RW.Writer, resp)
	}
}

func (c *Controller) handleAgentMessage(conn *protocol.Connection, msg protocol.Message) {
	switch msg.Type {
	case protocol.HEARTBEAT:
		c.updateAgentHeartbeat(conn.ID)
		fmt.Println("heartbeat from agent", conn.ID)

	default:
		fmt.Println("unknown agent message type")
	}
}

func (c *Controller) handleLBMessage(conn *protocol.Connection, msg protocol.Message) {

	fmt.Println(conn)

	switch msg.Type {

	default:
		fmt.Println("lb message")
	}
}

func (c *Controller) registerAgentMetadata(id, host, port string) {
	//TODO: find metadata from map that u need to implement and then just update heart beat only if it doesnt exist create new one

	c.mu.Lock()
	defer c.mu.Unlock()
	c.agentsInfo[id] = &protocol.AgentInfo{
		ID:            id,
		Host:          host,
		Port:          port,
		LastHeartbeat: time.Now(),
		Status:        protocol.Healthy,
		Microservices: make(map[string][]*protocol.MsInfo),
	}
}

func (c *Controller) unregisterAgentMetadata(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.agentsInfo, id)
}

func (c *Controller) updateAgentHeartbeat(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if info, ok := c.agentsInfo[id]; ok {
		info.Mu.Lock()
		info.LastHeartbeat = time.Now()
		info.Status = protocol.Healthy
		info.Mu.Unlock()
	}
}

// ! not used function might have errors or isnt even needed
// func (c *Controller) runHeartbeatChecker() {
// 	ticker := time.NewTicker(HeartbeatCheckPeriod)
// 	defer ticker.Stop()

// 	for range ticker.C {
// 		now := time.Now()
// 		var stale []string

// 		c.mu.RLock()
// 		for id, info := range c.agentsInfo {
// 			//only check agents that are connected to controller
// 			//if info.Conn != nil {
// 			continue
// 			//}
// 			info.Mu.RLock()
// 			last := info.LastHeartbeat
// 			info.Mu.RUnlock()
// 			if now.Sub(last) > HeartbeatTimeout {
// 				stale = append(stale, id)
// 			}
// 		}
// 		c.mu.RUnlock()

// 		for _, id := range stale {
// 			c.onAgentTimeout(id)
// 		}
// 	}
// }

// onAgentTimeout runs when an agent misses heartbeats for HeartbeatTimeout.
// Triggers safety measures and deep checking.
// ? when saving for example to db just skip unhealthy agents but keep thier metadata might be needed to restart them
func (c *Controller) onAgentTimeout(agentID string) {
	c.mu.Lock()
	info, ok := c.agentsInfo[agentID]
	if !ok {
		c.mu.Unlock()
		return // already removed
	}
	info.Mu.Lock()
	info.Status = protocol.Unhealthy
	info.Mu.Unlock()
	c.mu.Unlock()

	fmt.Printf("[SAFETY] Agent %s heartbeat timeout — initiating safety measures\n", agentID)

	conn := c.GetAgentByID(agentID)
	if conn != nil {
		conn.Close()
		c.Remove(conn, protocol.ConnAgent)
	}

	// Deep checking / cleanup (expand as needed)
	c.performDeepCheck(agentID)
}

// performDeepCheck runs additional safety checks when agent times out.
// Expand with: cleanup microservice refs, notify load balancers, metrics, etc.
func (c *Controller) performDeepCheck(agentID string) {
	// TODO: remove agent's microservices from c.microservices
	// TODO: notify load balancers to stop routing to this agent
	fmt.Printf("[DEEP CHECK] Agent %s removed — microservices and LBs may need rebalancing\n", agentID)
}

// ? may need this func if program expands
func (c *Controller) handleAgentDisconnect(agentID string) {

	c.mu.Lock()
	defer c.mu.Unlock()

	fmt.Println("Agent removed:", agentID)
}

// ################################## CORE FUNCTIONS #####################################

// #################################  API FUNCTIONS ################################

// #################################  API FUNCTIONS ################################

// ################################# AGENT FUNCTIONS ###############################

func (c *Controller) GetNextAgentID() uint32 {
	return atomic.AddUint32(&c.nextID, 1)
}

// createNewAgent spawns an agent process, assigns host:port, and registers it.
// Agent binary must accept --host and --port flags (e.g. ./agent --host localhost --port 10001).
func (c *Controller) createNewAgent(port string) (*protocol.AgentInfo, error) {
	id := fmt.Sprintf("%d", c.GetNextAgentID())

	cmd := exec.Command("../../cmd/agent/agent.exe", "--port", port)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start agent process: %w", err)
	}

	fmt.Println("Started process: ", cmd.Process.Pid)

	//TODO: later change hardcoded host, and port will be sent by agent
	agent := &protocol.AgentInfo{
		ID:            id,
		Host:          "localhost",
		Port:          port,
		Cmd:           cmd,
		Microservices: make(map[string][]*protocol.MsInfo),
	}

	conn, err := c.connectToAgent(agent)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("connect to agent: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.agentsInfo[conn.ID] = agent

	fmt.Printf("Agent started {ID:%s PID:%d}\n", id, cmd.Process.Pid)
	return agent, nil
}

func (c *Controller) connectToAgent(agent *protocol.AgentInfo) (*protocol.Connection, error) {
	address := net.JoinHostPort(agent.Host, agent.Port)

	timeout := time.After(120 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var nc net.Conn
	var err error
	for {
		select {
		case <-timeout:
			return nil, fmt.Errorf("timeout connecting to agent")

		case <-ticker.C:
			nc, err = net.DialTimeout("tcp", address, 500*time.Millisecond)
			if err == nil {
				fmt.Println("Connected to agent!")

				conn := protocol.NewConnection(nc)
				c.Register(conn, protocol.ConnAgent, agent.ID)

				return conn, nil
			}
		}
		fmt.Println("Attempt to connect to agent")
	}
}

// ---------------------------------- AGENT FUNCTIONS --------------------------------------

// ------------------------------ LOAD BALANCER FUNCTIONS ---------------------------------

func (c *Controller) handleLoadBalancer(rw *bufio.ReadWriter, request protocol.Message) {

}

// ------------------------------ LOAD BALANCER FUNCTIONS ---------------------------------

// ---------------------------------- MICROSERVICE FUNCTIONS --------------------------------------

func (c *Controller) findService(serviceType string) (*protocol.MsInfo, error) {

	c.mu.RLock()
	msArr, ok := c.microservices[serviceType]
	c.mu.RUnlock()

	if ok && len(msArr) > 0 {
		return msArr[0], nil
	}

	return c.createNewService(serviceType)
}

func (c *Controller) createNewService(serviceType string) (*protocol.MsInfo, error) {

	// Ensure at least one agent exists
	c.mu.RLock()
	hasAgent := len(c.agentsInfo) > 0
	c.mu.RUnlock()

	var agent *protocol.AgentInfo
	var err error

	if !hasAgent {
		agent, err = c.createNewAgent("2000")
		if err != nil {
			return nil, err
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

	if agent == nil {
		return nil, fmt.Errorf("no agents available: passed agent creation and iteration and still no agents were found")
	}

	// CREATE goes to agent's server port. Two paths:
	// - Spawned: agent.Conn is controller→agent, use it
	// - Agent-connected: agent.Conn is nil, controller DIALs agent.Host:agent.Port
	//   (ConnectionManager conn is for heartbeats only; CREATE must not use it)

	// mapping agent info to agent conn
	agentConn := c.agentsConn[agent.ID]

	if agentConn == nil {
		return nil, fmt.Errorf("Agents connection is nil")
	}

	request := protocol.Message{SessionID: agent.ID, Type: protocol.CREATE, Content: serviceType}
	if err := protocol.Send(agentConn.RW.Writer, request); err != nil {
		return nil, fmt.Errorf("send CREATE: %w", err)
	}

	response, err := protocol.Receive(agentConn.RW.Reader)
	if err != nil {
		return nil, err
	}

	ms := parseMsFromResponse(response.Content)

	c.mu.Lock()
	c.microservices[serviceType] = append(c.microservices[serviceType], ms)
	c.mu.Unlock()

	//make ms_info struct and return it
	return ms, nil
}

func parseMsFromResponse(content string) *protocol.MsInfo {
	ms := &protocol.MsInfo{}
	host, port, err := net.SplitHostPort(content)
	if err != nil {
		return ms
	}
	ms.Host, ms.Port = host, port
	return ms
}

// ---------------------------------- MICROSERVICE FUNCTIONS --------------------------------------
