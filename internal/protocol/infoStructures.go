package protocol

import (
	"os"
	"sync"
)

type ServiceType string

const (
	PingService     ServiceType = "PING"
	IdleService     ServiceType = "IDLE_WORK"
	UploadService   ServiceType = "FILE_UPLOAD"
	DownloadService ServiceType = "FILE_DOWNLOAD"
)

const (
	MAX_LB_PER_MSG_NODE = MAX_NODE_LOAD
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
