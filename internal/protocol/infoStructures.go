package protocol

import (
	"os"
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
	UploadService   ServiceType = "FILE_UPLOAD"
	DownloadService ServiceType = "FILE_DOWNLOAD"
)

type AgentInfo struct {
	ID     string
	NodeID string

	Host string
	Port string

	LastHeartbeat time.Time
	Status        NodeStatus

	Microservices map[ServiceType][]*MsInfo

	Process *os.Process

	Mu sync.RWMutex // protects mutable fields
}

type MsInfo struct {
	ID     string
	NodeID string

	PID int

	Host string
	Port string

	//TODO: later make this a serviceType not string
	Type ServiceType

	LastHeartbeat time.Time
	Status        NodeStatus

	Process *os.Process `json:"-"`

	Mu sync.RWMutex `json:"-"` // protects mutable fields
}

type LBalancerInfo struct {
	ID     string
	NodeID string

	Host string
	Port string

	LastHeartbeat time.Time
	Status        NodeStatus

	Microservices map[ServiceType][]*MsInfo

	Process *os.Process `json:"-"`

	Mu sync.RWMutex `json:"-"` // protects mutable fields
}

func (a *AgentInfo) Close() {
	a.Mu.Lock()
	defer a.Mu.Unlock()

	if a.Process != nil {
		a.Process.Kill()
		a.Process = nil
	}
}

func (ms *MsInfo) Close() {

	if ms.Process != nil {
		ms.Process.Kill()
		ms.Process = nil
	}
}
