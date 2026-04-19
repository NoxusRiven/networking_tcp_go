package protocol

import (
	"os/exec"
	"sync"
	"time"
)

type NodeStatus int

const (
	Healthy NodeStatus = iota
	Unhealthy
	Unknown
)

type ServiceType string

const (
	PingService     ServiceType = "PING"
	UpladService    ServiceType = "FILE_UPLOAD"
	DownloadService ServiceType = "FILE_DOWNLOAD"
)

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
