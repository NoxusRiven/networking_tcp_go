package protocol

import (
	"networking/tcp/internal/utils"
	"time"
)

//TODO: lil rework should be done to this idea
//! old interface

const (
	MAX_NODE_LOAD = 2
)

type NodeStatus int

const (
	Healthy NodeStatus = iota
	Unhealthy
	Busy
	Unknown
)

type Node interface {
	ReceiveHeartBeat(Message, *Connection)
	AsyncEvent(Message, *Connection)
	HandShake(*Connection)
	String() string
}

type NodeBase struct {
	ID     string
	NodeID string

	Host string
	Port string

	LastHeartbeat time.Time
	Status        NodeStatus

	CurrentLoad utils.Counter
}

func (n *NodeBase) IsLoadStable() bool {
	return n.CurrentLoad.GetValue() < MAX_NODE_LOAD
}

func (n *NodeBase) MarkAsHealthy(busyOverride bool) {
	if busyOverride || n.Status != Busy {
		n.Status = Healthy
	}
}

func (n *NodeBase) MarkAsUnhealthy() {
	n.Status = Unhealthy
}

//idea behind is that some nodes like CLI are pure nodes without Worker or Manager methods, and some only Work and doesnt Manage
//? new api still in progress

type WorkerNode interface {
	AsyncEvent(Message, *Connection)
	String() string
}

type ManagerNode interface {
	ReceiveHeartBeat(Message, *Connection)
}
