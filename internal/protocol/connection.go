package protocol

import (
	"bufio"
	"fmt"
	"net"
	crypto "networking/tcp/internal/cryptography"
	"sync"
	"time"
)

type ConnectionType int

const (
	ConnUnknown ConnectionType = iota
	ConnAPI
	ConnAgent
	ConnLB
)

type Connection struct {
	ID string

	Conn net.Conn
	RW   *bufio.ReadWriter

	pending map[string]chan Message

	mu sync.Mutex

	closeOnce sync.Once
}

func NewConnection(nc net.Conn) *Connection {
	return &Connection{
		Conn:    nc,
		pending: make(map[string]chan Message),
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

func (c *Connection) ReceiveLoop(node Node) {
	fmt.Println("[Receive Loop] started receive loop!")
	for {
		msg, err := Receive(c.RW.Reader)
		if err != nil {
			fmt.Println("[Receive loop]: ", err)
		}

		if ch, ok := c.pending[msg.ID]; ok {
			ch <- msg
			fmt.Println("[Receive Loop]: received and deleting", msg.ID)
			delete(c.pending, msg.ID)
			continue
		}

		switch msg.Type {
		case HEARTBEAT:
			node.HandleHeartBeat(msg) //? maybe just need content bcs you know its heartbeat
		default:
			node.NodeAsyncEvent(msg)
		}

	}
}

func (c *Connection) SendRequest(msg Message) (Message, error) {
	msg.ID = crypto.GenerateID(4)

	ch := make(chan Message, 1)

	c.mu.Lock()
	c.pending[msg.ID] = ch
	fmt.Println("[Send Request] started pending on key", msg.ID)
	c.mu.Unlock()

	c.mu.Lock()
	Send(c.RW.Writer, msg)
	c.mu.Unlock()

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(5 * time.Second):
		return Message{}, fmt.Errorf("timeout")
	}
}
