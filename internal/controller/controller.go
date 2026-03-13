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
	"time"
)

/**

*! requests to load balancer should be in queue channel like in api gateway

 */

const (
	REQUEST_PORT = "8888"
	SYSTEM_PORT  = "9000"
)

const (
	BASE_PORT_AGENT     uint16 = 10000
	BASE_PORT_LBALANCER uint16 = 15000

	HeartbeatTimeout     = 3 * time.Second
	HeartbeatCheckPeriod = 500 * time.Millisecond
)

type ClientType int

const (
	ClientUnknown ClientType = iota
	ClientAPI
	ClientAgent
	ClientLB
)

// ------------------------------------ STRUCTURES ----------------------------------------

type Controller struct {
	apiListener    net.Listener
	systemListener net.Listener

	agents        map[string]*protocol.AgentInfo
	microservices map[string][]*protocol.MsInfo

	connManager *ConnectionManager

	mu sync.RWMutex

	nextID        uint32 // id tracker of agent and load balancer
	nextAgentPort uint32 // for assignAgentAddress (controller assigns host:port)
}

// ? might need to extract this later to sepatate file
type Connection struct {
	ID   string
	Type ClientType

	Conn net.Conn
	RW   *bufio.ReadWriter

	closeOnce sync.Once
}

type ConnectionManager struct {
	api        map[string]*Connection
	agents     map[string]*Connection
	lBalancers map[string]*Connection

	mu sync.RWMutex
}

// ------------------------------------ STRUCTURES ----------------------------------------

// ################################ CONNECTION FUNCTIONS ################################

func (c *Connection) Close() {
	c.closeOnce.Do(func() {
		c.Conn.Close()
	})
}

func NewConectionManager() *ConnectionManager {
	return &ConnectionManager{
		api:        make(map[string]*Connection),
		agents:     make(map[string]*Connection),
		lBalancers: make(map[string]*Connection),
	}
}

func (cm *ConnectionManager) Register(conn *Connection) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	conn.ID = crypto.GenerateID()

	switch conn.Type {
	case ClientAPI:
		cm.api[conn.ID] = conn
	case ClientAgent:
		cm.agents[conn.ID] = conn
	case ClientLB:
		cm.lBalancers[conn.ID] = conn
	}
}

func (cm *ConnectionManager) Remove(conn *Connection) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	switch conn.Type {
	case ClientAPI:
		delete(cm.api, conn.ID)

	case ClientAgent:
		delete(cm.agents, conn.ID)

	case ClientLB:
		delete(cm.lBalancers, conn.ID)
	}
}

// ? might make this a single function that takes 1 param and depending on that param switches between all manager containers
func (cm *ConnectionManager) GetAgents() []*Connection {
	// cm.mu.Lock()
	// defer cm.mu.Unlock()
	out := make([]*Connection, 0, len(cm.agents))
	for _, a := range cm.agents {
		out = append(out, a)
	}

	return out
}

func (cm *ConnectionManager) GetAgentByID(id string) *Connection {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.agents[id]
}

// ################################ CONNECTION FUNCTIONS ################################

// ---------------------------------- CORE FUNCTIONS --------------------------------------

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
		agents:         make(map[string]*protocol.AgentInfo),
		microservices:  make(map[string][]*protocol.MsInfo),
		connManager:    NewConectionManager(),
		nextID:         0,
	}, nil
}

// starts and accepts api and system connections
func (c *Controller) Start() {
	fmt.Println("Controller started")

	go c.acceptAPI()
	go c.acceptSystem()
	go c.runHeartbeatChecker()
}

func (c *Controller) acceptAPI() {
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

func (c *Controller) newConnection(nc net.Conn) *Connection {
	return &Connection{
		Conn: nc,
		RW: bufio.NewReadWriter(
			bufio.NewReader(nc),
			bufio.NewWriter(nc),
		),
	}
}

// ? maybe destinguish conn and session id with prefix conn/sess-id
func (c *Controller) handleAPIConn(nc net.Conn) {
	conn := c.newConnection(nc)
	defer conn.Close()

	msg, err := protocol.Receive(conn.RW.Reader)
	if err != nil {
		fmt.Println("api register error:", err)
		return
	}

	if msg.Type != protocol.REG_API {
		fmt.Println("expected type REGISTER API")
		return
	}

	conn.Type = ClientAPI

	// Register generates and sets conn ID
	c.connManager.Register(conn)
	defer c.connManager.Remove(conn)

	fmt.Println("API connected:", conn.ID)

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

// TODO: finish refactoring from here onward
func (c *Controller) handleSystemConn(nc net.Conn) {
	conn := c.newConnection(nc)
	defer conn.Close()

	msg, err := protocol.Receive(conn.RW.Reader)
	if err != nil {
		fmt.Println("system register error:", err)
		return
	}

	switch msg.Type {
	case protocol.REG_AGENT:
		conn.Type = ClientAgent
		host, port := c.assignAgentAddress()

		c.connManager.Register(conn)
		defer c.connManager.Remove(conn)
		c.registerAgentMetadata(conn.ID, host, port)
		defer c.unregisterAgentMetadata(conn.ID)

		fmt.Print("Agent registered ")

	case protocol.REG_LB:
		conn.Type = ClientLB
		fmt.Print("LoadBalancer registered ")
	default:
		fmt.Println("Unknown system client")
		return
	}

	if conn.Type != ClientAgent {
		c.connManager.Register(conn)
		defer c.connManager.Remove(conn)
	}

	fmt.Print(conn.ID)

	for {
		req, err := protocol.Receive(conn.RW.Reader)
		if err != nil {
			fmt.Println("system node disconected")
			//? handle agent disconect func, but i think it is unesecary

			return
		}

		switch conn.Type {
		case ClientAgent:
			c.handleAgentMessage(conn, req)
		case ClientLB:
			c.handleLBMessage(conn, req)
		}

	}
}

func (c *Controller) handleAPIRequst(conn *Connection, msg protocol.Message) {
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

func (c *Controller) handleAgentMessage(conn *Connection, msg protocol.Message) {
	switch msg.Type {
	case protocol.HEARTBEAT:
		c.updateAgentHeartbeat(conn.ID)
		fmt.Println("heartbeat from agent", conn.ID)

	default:
		fmt.Println("unknown agent message type")
	}
}

func (c *Controller) handleLBMessage(conn *Connection, msg protocol.Message) {

	switch msg.Type {

	default:
		fmt.Println("lb message")
	}
}

// assignAgentAddress returns the next host:port the controller assigns to an agent.
func (c *Controller) assignAgentAddress() (host, port string) {
	n := atomic.AddUint32(&c.nextAgentPort, 1)
	//TODO: later make it logicly choose host
	return "localhost", fmt.Sprintf("%d", BASE_PORT_AGENT+uint16(n-1))
}

func (c *Controller) registerAgentMetadata(id, host, port string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.agents[id] = &protocol.AgentInfo{
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
	delete(c.agents, id)
}

func (c *Controller) updateAgentHeartbeat(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if info, ok := c.agents[id]; ok {
		info.Mu.Lock()
		info.LastHeartbeat = time.Now()
		info.Status = protocol.Healthy
		info.Mu.Unlock()
	}
}

func (c *Controller) runHeartbeatChecker() {
	ticker := time.NewTicker(HeartbeatCheckPeriod)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		var stale []string

		c.mu.RLock()
		for id, info := range c.agents {
			//only check agents that are connected to controller
			if info.Conn != nil {
				continue
			}
			info.Mu.RLock()
			last := info.LastHeartbeat
			info.Mu.RUnlock()
			if now.Sub(last) > HeartbeatTimeout {
				stale = append(stale, id)
			}
		}
		c.mu.RUnlock()

		for _, id := range stale {
			c.onAgentTimeout(id)
		}
	}
}

// onAgentTimeout runs when an agent misses heartbeats for HeartbeatTimeout.
// Triggers safety measures and deep checking.
// ? when saving for example to db just skip unhealthy agents but keep thier metadata might be needed to restart them
func (c *Controller) onAgentTimeout(agentID string) {
	c.mu.Lock()
	info, ok := c.agents[agentID]
	if !ok {
		c.mu.Unlock()
		return // already removed
	}
	info.Mu.Lock()
	info.Status = protocol.Unhealthy
	info.Mu.Unlock()
	c.mu.Unlock()

	fmt.Printf("[SAFETY] Agent %s heartbeat timeout — initiating safety measures\n", agentID)

	conn := c.connManager.GetAgentByID(agentID)
	if conn != nil {
		conn.Close()
		c.connManager.Remove(conn)
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

// ---------------------------------- CORE FUNCTIONS --------------------------------------

// #################################  API FUNCTIONS ################################

// #################################  API FUNCTIONS ################################
// ################################# AGENT FUNCTIONS ###############################

func (c *Controller) GetNextAgentID() uint32 {
	return atomic.AddUint32(&c.nextID, 1)
}

// createNewAgent spawns an agent process, assigns host:port, and registers it.
// Agent binary must accept --host and --port flags (e.g. ./agent --host localhost --port 10001).
func (c *Controller) createNewAgent() (*protocol.AgentInfo, error) {
	host, port := c.assignAgentAddress()
	id := fmt.Sprintf("%d", c.GetNextAgentID())

	cmd := exec.Command("./tcp/cmd/agent", "--port", port)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start agent process: %w", err)
	}

	agent := &protocol.AgentInfo{
		ID:            id,
		Host:          host,
		Port:          port,
		Cmd:           cmd,
		Microservices: make(map[string][]*protocol.MsInfo),
	}

	if err := connectToAgent(agent); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("connect to agent: %w", err)
	}
	defer agent.Close()

	c.mu.Lock()
	defer c.mu.Unlock()
	c.agents[agent.ID] = agent

	fmt.Printf("Agent started {ID:%s PID:%d Port:%s}\n", id, cmd.Process.Pid, port)
	return agent, nil
}

func connectToAgent(agent *protocol.AgentInfo) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(agent.Host, agent.Port), 5*time.Second)
	if err != nil {
		return err
	}

	agent.Conn = conn

	agent.RW = bufio.NewReadWriter(
		bufio.NewReader(conn),
		bufio.NewWriter(conn),
	)

	return nil
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
	hasAgent := len(c.agents) > 0
	c.mu.RUnlock()
	if !hasAgent {
		if _, err := c.createNewAgent(); err != nil {
			return nil, err
		}
	}

	// TODO: choose agent by load (least busy, etc.)
	c.mu.RLock()
	var agent *protocol.AgentInfo
	for _, a := range c.agents {
		agent = a
		break
	}
	c.mu.RUnlock()

	if agent == nil {
		return nil, fmt.Errorf("no agents available")
	}

	// CREATE goes to agent's server port. Two paths:
	// - Spawned: agent.Conn is controller→agent, use it
	// - Agent-connected: agent.Conn is nil, controller DIALs agent.Host:agent.Port
	//   (ConnectionManager conn is for heartbeats only; CREATE must not use it)
	var writer *bufio.Writer
	var reader *bufio.Reader

	if agent.Conn != nil {
		agent.Mu.Lock()
		_ = agent.Conn.SetDeadline(time.Now().Add(10 * time.Second))
		writer, reader = agent.RW.Writer, agent.RW.Reader
		agent.Mu.Unlock()
	} else {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(agent.Host, agent.Port), 5*time.Second)
		if err != nil {
			return nil, fmt.Errorf("dial agent %s: %w", agent.ID, err)
		}
		defer conn.Close()
		rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
		writer, reader = rw.Writer, rw.Reader
		if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
			return nil, err
		}
	}

	request := protocol.Message{SessionID: agent.ID, Type: protocol.CREATE, Content: serviceType}
	if err := protocol.Send(writer, request); err != nil {
		return nil, fmt.Errorf("send CREATE: %w", err)
	}

	response, err := protocol.Receive(reader)
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
