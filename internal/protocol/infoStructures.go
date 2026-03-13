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

type AgentInfo struct {
	ID   string
	Host string
	Port string

	LastHeartbeat time.Time
	Status        NodeStatus

	Microservices map[string][]*MsInfo

	Conn net.Conn
	RW   *bufio.ReadWriter

	Cmd *exec.Cmd

	Mu sync.RWMutex // protects mutable fields
}

type MsInfo struct {
	Agent     AgentInfo
	LBalancer LBalancerInfo

	Host string
	Port string

	status NodeStatus

	Conn net.Conn
	RW   *bufio.ReadWriter

	Cmd *exec.Cmd
}

type LBalancerInfo struct {
	Host          string
	Port          string
	Microservices map[string][]*MsInfo
}

func (a *AgentInfo) Close() {
	a.Mu.Lock()
	defer a.Mu.Unlock()

	if a.Conn != nil {
		a.Conn.Close()
		a.Conn = nil
	}

	if a.Cmd.Process != nil {
		a.Cmd.Process.Kill()
		a.Cmd = nil
	}
}

func (ms *MsInfo) Close() {
	if ms.Conn != nil {
		ms.Conn.Close()
		ms.Conn = nil
	}

	if ms.Cmd.Process != nil {
		ms.Cmd.Process.Kill()
		ms.Cmd = nil
	}
}
