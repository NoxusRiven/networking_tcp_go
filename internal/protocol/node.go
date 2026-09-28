package protocol

type Node interface {
	ReceiveHeartBeat(Message, *Connection)
	AsyncEvent(Message, *Connection)
}
