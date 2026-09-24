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

	Mu sync.Mutex
	RWmu sync.RWMutex

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
	//fmt.Println("[Receive Loop] started receive loop!")
	for {
		msg, err := Receive(c.RW.Reader)
		if err != nil {
			fmt.Println("[Receive loop][ERROR]", c.ID, ": ", err)
			delete(c.pending, msg.ID)
			return
		}

		if ch, ok := c.pending[msg.ID]; ok {
			ch <- msg
			//fmt.Println("[Receive Loop]: received and deleting", msg.ID)
			delete(c.pending, msg.ID)
			continue
		}

		switch msg.Type {
		case HEARTBEAT:
			go node.HandleHeartBeat(msg) //? maybe just need content bcs you know its heartbeat
		default:
			go node.NodeAsyncEvent(msg, c)
		}
	}
}

func (c *Connection) SendRequest(msg Message) (Message, error) {
	msg.ID = crypto.GenerateID(4)

	ch := make(chan Message, 1)

	c.Mu.Lock()
	c.pending[msg.ID] = ch
	fmt.Println("[Send Request] started pending on key", msg.ID)
	c.Mu.Unlock()

	fmt.Println("[Send Request] full message: ", msg)
	c.Mu.Lock()
	Send(c.RW.Writer, msg)
	c.Mu.Unlock()

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(5 * time.Second):
		c.Mu.Lock()
		delete(c.pending, msg.ID)
		c.Mu.Unlock()

		return Message{}, fmt.Errorf("timeout - %v", msg)
	}
}

func (c *Connection) SendRequestNew(msg Message) (<-chan Message, error) {
	msg.ID = crypto.GenerateID(4)

	ch := make(chan Message, 8)

	c.Mu.Lock()
	c.pending[msg.ID] = ch
	fmt.Println("[Send Request] active channel on key", msg.ID)
	c.Mu.Unlock()

	fmt.Println("[Send Request] full message: ", msg)
	c.Mu.Lock()
	err := Send(c.RW.Writer, msg)
	c.Mu.Unlock()

	if err != nil {
		c.Mu.Lock()
		delete(c.pending, msg.ID)
		close(ch)
		c.Mu.Unlock()
		return nil, err
	}
	return ch, nil
}

func (c *Connection) ReceiveLoopNew(node Node) {
	for {
		msg, err := Receive(c.RW.Reader)
		if err != nil {
			fmt.Println("[Receive loop][ERROR]", c.ID, ": ", err)
			delete(c.pending, msg.ID)
			return
		}

		fmt.Println("Received message:", msg)
		if ch, ok := c.pending[msg.ID]; ok {

			ch <- msg
			if !msg.IsStream {
				fmt.Println("Not stream")
				c.Mu.Lock()
				delete(c.pending, msg.ID)
				close(ch)
				c.Mu.Unlock()
			}
			continue
		}

		switch msg.Type {
		case HEARTBEAT:
			go node.HandleHeartBeat(msg) //? maybe just need content bcs you know its heartbeat
		default:
			go node.NodeAsyncEvent(msg, c)
		}
	}
}
