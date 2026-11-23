package protocol

import (
	"os"
	"sync"
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
	IdleService     ServiceType = "IDLE_WORK"
	UploadService   ServiceType = "FILE_UPLOAD"
	DownloadService ServiceType = "FILE_DOWNLOAD"
)

const (
	MAX_NODE_LOAD = 5
)

type AgentInfo struct {
	Base NodeBase

	Microservices map[ServiceType][]*MsInfo

	Process *os.Process `json:"-"`

	Mu sync.RWMutex `json:"-"` // protects mutable fields

	MsCurrPort string
}

type MsInfo struct {
	Base NodeBase

	Type ServiceType

	PID     int
	Process *os.Process `json:"-"`

	Mu sync.RWMutex `json:"-"` // protects mutable fields
}

type LBalancerInfo struct {
	Base NodeBase

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
