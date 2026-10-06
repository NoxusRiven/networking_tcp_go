package protocol

//TODO: lil rework should be done to this idea
//! old interface
type Node interface {
	ReceiveHeartBeat(Message, *Connection)
	AsyncEvent(Message, *Connection)
	String() string
}

//idea behind is that some nodes like CLI are pure nodes without Worker or Manager methods, and some only Work and doesnt Manage
//? new api still in progress
type NodeBase struct {
	ID string

	Host string
	Port string
}

type WorkerNode interface {
	AsyncEvent(Message, *Connection)
	String() string
}

type ManagerNode interface {
	ReceiveHeartBeat(Message, *Connection)
}
