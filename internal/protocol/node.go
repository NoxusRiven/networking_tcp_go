package protocol

type Node interface {
	HandleHeartBeat(Message)
	NodeAsyncEvent(Message)
}
