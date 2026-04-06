package controller

import (
	"fmt"
	"net"
	crypto "networking/tcp/internal/cryptography"
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

// TODO: if agent is in the same host as controller he can choose ports otherwise controller sets boundry or just a free port and agents sends back wich port he got in remote host
// TODO: NOW! for now make lb loadbalancing in controller, after that change that so controller doesnt get client requests, only in edege cases api will send requests to controller
type Controller struct {
	apiListener    net.Listener
	systemListener net.Listener

	//? if controller will connect to more then 1 api make it same as agent storage
	apiConn map[*protocol.Connection]struct{}

	agentsInfo map[string]*protocol.AgentInfo
	agentsConn map[string]*protocol.Connection

	lbInfo map[string]*protocol.LBalancerInfo
	lbConn map[string]*protocol.Connection

	microservices map[string][]*protocol.MsInfo

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
		lbConn: make(map[string]*protocol.Connection),

		microservices: make(map[string][]*protocol.MsInfo),

		nextID: 0,
	}, nil
}

// starts and accepts api and system connections
func (c *Controller) Start() {
	fmt.Println("[CONTROLLER]: Controller started")

	go c.acceptAPI()
	go c.acceptSystem()
	//go c.runHeartbeatChecker()
}

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
		c.lbConn[nodeID] = conn
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

func (c *Controller) acceptSystem() {
	fmt.Println("[CONTROLLER]: Listening for System on", c.systemListener.Addr())

	for {
		conn, err := c.systemListener.Accept()
		if err != nil {
			fmt.Println("[CONTROLLER]: System accept error:", err)
			continue
		}

		fmt.Println("[CONTROLLER]: Accepted new system node connection")

		go c.handleSystemConn(conn)
	}
}

func (c *Controller) handleSystemConn(nc net.Conn) {
	conn := protocol.NewConnection(nc)
	defer conn.Close()

	msg, err := protocol.Receive(conn.RW.Reader)
	if err != nil {
		fmt.Println("[CONTROLLER]: system register error:", err)
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

		fmt.Print("[CONTROLLER]: Agent registered ")

	case protocol.REG_LB:
		fmt.Print("[CONTROLLER]: LoadBalancer registered ")
	default:
		fmt.Println("[CONTROLLER]: Unknown system client")
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
			fmt.Println("[CONTROLLER]: system node disconected")
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

// performDeepCheck runs additional safety checks when agent times out.
// Expand with: cleanup microservice refs, notify load balancers, metrics, etc.
func (c *Controller) performDeepCheck(agentID string) {
	// TODO: notify load balancer to stop routing to this agent
	fmt.Printf("[DEEP CHECK] Agent %s removed — microservices and LBs may need rebalancing\n", agentID)
}

// TODO: controller creates agent (also connect) then after when sending request to agent about creating new service, agent confirms creating ms and then message is sent to lb about new ms and to update data, so node is always in sync
func (c *Controller) createNewMessageNode(agentPort string, lbPort string) (*protocol.AgentInfo, *protocol.LBalancerInfo, error) {
	nodeID := crypto.GenerateID()

	agent, err := c.createNewAgent(agentPort)
	if err != nil {
		return nil, nil, err
	}

	//TODO: handle cleaning up agent (no point for agent that doesnt have lb)
	lb, err := c.createNewLoadBalancer(lbPort)
	if err != nil {
		return nil, nil, err
	}

	agent.NodeID = nodeID
	lb.NodeID = nodeID

	//only returns if both of them succeded
	return agent, lb, nil
}

// ################################## CORE FUNCTIONS #####################################

// #################################  API FUNCTIONS ################################

func (c *Controller) acceptAPI() {
	fmt.Println("[CONTROLLER]: Listening for API on", c.apiListener.Addr())

	for {
		conn, err := c.apiListener.Accept()
		if err != nil {
			fmt.Println("[CONTROLLER]: API accept error:", err)
			continue
		}

		fmt.Println("[CONTROLLER]: Accepted new api connection")

		go c.handleAPIConn(conn)
	}
}

func (c *Controller) handleAPIConn(nc net.Conn) {
	conn := protocol.NewConnection(nc)
	defer conn.Close()

	msg, err := protocol.Receive(conn.RW.Reader)
	if err != nil {
		fmt.Println("[CONTROLLER]: api register error:", err)
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

	fmt.Println("[CONTROLLER]: API connected:", conn.ID)

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
			fmt.Println("[CONTROLLER]: api disconected:", err)
			return
		}

		// Sequential: preserves request/response order. For concurrency, use a per-conn
		// request queue and worker that serializes responses.
		c.handleAPIRequst(conn, req)

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

		response, err := c.messageService(ms, msg)
		if err != nil {
			return
		}

		fmt.Println("[COTROLLER]: response from ms", response)

		protocol.Send(conn.RW.Writer, response)

	default:

		resp := protocol.Message{
			Type:    protocol.PING,
			Code:    protocol.ERROR,
			Content: "Invalid message type",
		}

		protocol.Send(conn.RW.Writer, resp)
	}
}

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
	//TODO: for now simple ridirection stdout and err but later make logger and use pipeing
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("[CONTROLLER]: start agent process: %w\n", err)
	}

	fmt.Println("[CONTROLLER]: Started agent process: ", cmd.Process.Pid)

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
		return nil, fmt.Errorf("[CONTROLLER]: connect to agent: %w\n", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.agentsInfo[conn.ID] = agent

	fmt.Printf("[CONTROLLER]: Agent started {ID:%s PID:%d}\n", id, cmd.Process.Pid)
	return agent, nil
}

func (c *Controller) connectToAgent(agent *protocol.AgentInfo) (*protocol.Connection, error) {
	address := net.JoinHostPort(agent.Host, agent.Port)

	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var nc net.Conn
	var err error
	for {
		select {
		case <-timeout:
			return nil, fmt.Errorf("[CONTROLLER]: timeout connecting to agent\n")

		case <-ticker.C:
			nc, err = net.DialTimeout("tcp", address, 500*time.Millisecond)
			if err == nil {
				fmt.Println("[CONTROLLER]: Connected to agent!")

				conn := protocol.NewConnection(nc)
				c.Register(conn, protocol.ConnAgent, agent.ID)

				return conn, nil
			}
		}
		fmt.Println("[CONTROLLER]: Attempt to connect to agent")
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
		if a == nil || a.Cmd == nil || a.Cmd.Process == nil {
			continue
		}
		a.Cmd.Process.Kill()
	}
}

func (c *Controller) handleAgentMessage(conn *protocol.Connection, msg protocol.Message) {
	switch msg.Type {
	case protocol.HEARTBEAT:
		c.updateAgentHeartbeat(conn.ID)
		fmt.Println("[CONTROLLER]: heartbeat from agent", conn.ID)

	default:
		fmt.Println("[CONTROLLER]: unknown agent message type")
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

	conn := c.GetAgentConnByID(agentID)
	if conn != nil {
		conn.Close()
		c.Remove(conn, protocol.ConnAgent)
	}

	// Deep checking / cleanup (expand as needed)
	c.performDeepCheck(agentID)
}

// ? may need this func if program expands
func (c *Controller) handleAgentDisconnect(agentID string) {

	c.mu.Lock()
	defer c.mu.Unlock()

	fmt.Println("[CONTROLLER]: Agent removed:", agentID)
}

// ################################# AGENT FUNCTIONS ###################################

// ################################ LOAD BALANCER FUNCTIONS ###################################
func (c *Controller) createNewLoadBalancer(port string) (*protocol.LBalancerInfo, error) {
	id := fmt.Sprintf("%d", c.GetNextAgentID())

	cmd := exec.Command("../../cmd/loadbalancer/lb.exe", "--port", port)
	//TODO: for now simple ridirection stdout and err but later make logger and use pipeing
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("[CONTROLLER]: start loadbalancer process: %w\n", err)
	}

	fmt.Println("[CONTROLLER]: Started lb process: ", cmd.Process.Pid)

	//TODO: later change hardcoded host, and port will be sent by lb
	lb := &protocol.LBalancerInfo{
		ID:            id,
		Host:          "localhost",
		Port:          port,
		Cmd:           cmd,
		Microservices: make(map[string][]*protocol.MsInfo),
	}

	conn, err := c.connectToLB(lb)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("[CONTROLLER]: connect to loadbalancer: %w\n", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.lbInfo[conn.ID] = lb

	fmt.Printf("[CONTROLLER]: Loadbalancer started {ID:%s PID:%d}\n", id, cmd.Process.Pid)
	return lb, nil
}

func (c *Controller) connectToLB(lb *protocol.LBalancerInfo) (*protocol.Connection, error) {
	address := net.JoinHostPort(lb.Host, lb.Port)

	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var nc net.Conn
	var err error
	for {
		select {
		case <-timeout:
			return nil, fmt.Errorf("[CONTROLLER]: timeout connecting to loadbalancer\n")

		case <-ticker.C:
			nc, err = net.DialTimeout("tcp", address, 500*time.Millisecond)
			if err == nil {
				fmt.Println("[CONTROLLER]: Connected to agent!")

				conn := protocol.NewConnection(nc)
				c.Register(conn, protocol.ConnLB, lb.ID)

				return conn, nil
			}
		}
		fmt.Println("[CONTROLLER]: Attempt to connect to agent")
	}
}

func (c *Controller) handleLBMessage(conn *protocol.Connection, msg protocol.Message) {

	fmt.Println(conn)

	switch msg.Type {

	default:
		fmt.Println("[CONTROLLER]: lb message")
	}
}

// ################################ LOAD BALANCER FUNCTIONS ###################################

// ################################ MICROSERVICE FUNCTIONS ###################################
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
	hasLB := len(c.lbInfo) > 0
	c.mu.RUnlock()

	var agent *protocol.AgentInfo
	var lb *protocol.LBalancerInfo
	var err error

	if !hasAgent || !hasLB {
		//TODO: later change to createNewMessageNode
		agent, lb, err = c.createNewMessageNode(BASE_PORT_AGENT, BASE_PORT_LBALANCER)
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

	if agent == nil || lb == nil {
		return nil, fmt.Errorf("[CONTROLLER]: no message node available: passed agent creation and iteration and still no agents were found (a: %v, l: %v)\n", agent, lb)
	}

	// mapping agent and lb info to agent conn
	agentConn := c.agentsConn[agent.ID]
	lbConn := c.lbConn[lb.ID]

	if agentConn == nil || lbConn == nil {
		return nil, fmt.Errorf("[CONTROLLER]: One of message node connection is nil ag: %v, lb: %v\n", agentConn, lbConn)
	}

	// message agent to create service
	request := protocol.Message{SessionID: agent.ID, Type: protocol.CREATE, Content: serviceType}
	if err := protocol.Send(agentConn.RW.Writer, request); err != nil {
		return nil, fmt.Errorf("[CONTROLLER]: send CREATE: %w\n", err)
	}

	response, err := protocol.Receive(agentConn.RW.Reader)
	if err != nil {
		return nil, err
	}

	ms := parseMsFromResponse(response.Content)
	ms.ID = crypto.GenerateID()
	ms.NodeID = agent.NodeID
	ms.Type = serviceType

	//inform load balancer about newly created service
	request = protocol.Message{SessionID: response.SessionID, Type: protocol.UPDATE, Content: string(ms.ID + ";" + ms.Host + ";" + ms.Port + ";" + ms.NodeID + ";" + ms.Type)}
	if err = protocol.Send(lbConn.RW.Writer, request); err != nil {
		return nil, fmt.Errorf("[CONTROLLER]: send ms info to lb: %w\n", err)
	}

	response, err = protocol.Receive(lbConn.RW.Reader)
	if err != nil {
		return nil, err
	}

	if response.Code != protocol.SUCCESS {
		return nil, fmt.Errorf("[CONTROLLER]: Bad code type %d, message: %s\n", response.Code, response.Content)
	}

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

func (c *Controller) messageService(ms *protocol.MsInfo, msg protocol.Message) (protocol.Message, error) {
	nodeID := ms.NodeID
	var lb *protocol.LBalancerInfo
	for _, l := range c.lbInfo {
		if l.NodeID == nodeID {
			lb = l
			break
		}
	}

	if lb == nil {
		return protocol.Message{}, fmt.Errorf("Loadbalancer with the same node id as ms not found\n")
	}

	conn := c.lbConn[lb.ID]

	if conn == nil {
		return protocol.Message{}, fmt.Errorf("Loadbalancer connection not found\n")
	}

	err := protocol.Send(conn.RW.Writer, msg)
	if err != nil {
		return protocol.Message{}, err
	}

	return protocol.Receive(conn.RW.Reader)
}

// ################################ MICROSERVICE FUNCTIONS ###################################
