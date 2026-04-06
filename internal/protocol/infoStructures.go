package protocol

import (
	"bufio"
	"net"
	"os/exec"
	"sync"
	"time"
)

// AgentInfo holds metadata for an agent. Use Host+Port when controller dials
// agent's server for CREATE/control requests.
//
// Connection ownership:
//   - New model (agent connects to controller): connection lives in ConnectionManager.
//     Conn and RW are nil.
//   - Legacy (controller spawns agent): Conn and RW are set. Deprecated.

type NodeStatus int

const (
	Healthy NodeStatus = iota
	Unhealthy
	Unknown
)

type ConnectionType int

const (
	ConnUnknown ConnectionType = iota
	ConnAPI
	ConnAgent
	ConnLB
)

type ServiceType string

const (
	PingService     ServiceType = "PING"
	UpladService    ServiceType = "FILE_UPLOAD"
	DownloadService ServiceType = "FILE_DOWNLOAD"
)

// if connection has id it was registered
type Connection struct {
	ID string

	Conn net.Conn
	RW   *bufio.ReadWriter

	mu sync.Mutex

	closeOnce sync.Once
}

type AgentInfo struct {
	ID     string
	NodeID string

	Host string
	Port string

	LastHeartbeat time.Time
	Status        NodeStatus

	Microservices map[string][]*MsInfo

	Cmd *exec.Cmd

	Mu sync.RWMutex // protects mutable fields
}

type MsInfo struct {
	ID     string
	NodeID string

	Host string
	Port string

	//TODO: later make this a serviceType not string
	Type string

	status NodeStatus

	Cmd *exec.Cmd
}

type LBalancerInfo struct {
	ID     string
	NodeID string

	Host string
	Port string

	LastHeartbeat time.Time
	Status        NodeStatus

	Microservices map[string][]*MsInfo

	Cmd *exec.Cmd

	Mu sync.RWMutex // protects mutable fields
}

func NewConnection(nc net.Conn) *Connection {
	return &Connection{
		Conn: nc,
		RW: bufio.NewReadWriter(
			bufio.NewReader(nc),
			bufio.NewWriter(nc),
		),
	}
}

func (c *Connection) Close() {
	c.closeOnce.Do(func() {
		c.Conn.Close()
	})
}

func (a *AgentInfo) Close() {
	a.Mu.Lock()
	defer a.Mu.Unlock()

	if a.Cmd.Process != nil {
		a.Cmd.Process.Kill()
		a.Cmd = nil
	}
}

func (ms *MsInfo) Close() {

	if ms.Cmd.Process != nil {
		ms.Cmd.Process.Kill()
		ms.Cmd = nil
	}
}
