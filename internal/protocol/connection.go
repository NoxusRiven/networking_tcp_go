package protocol

import (
	"bufio"
	"fmt"
	"net"
	crypto "networking/tcp/internal/crypto"
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

	Nc net.Conn
	RW *bufio.ReadWriter

	pending map[string]chan Message

	Mu   sync.Mutex
	RWmu sync.RWMutex

	closeOnce sync.Once
}

func NewConnection(nc net.Conn) *Connection {
	return &Connection{
		Nc:      nc,
		pending: make(map[string]chan Message),
		RW: bufio.NewReadWriter(
			bufio.NewReader(nc),
			bufio.NewWriter(nc),
		),
	}
}

func (c *Connection) Close() {
	c.closeOnce.Do(func() {
		c.Nc.Close()
	})
}

func (c *Connection) ReceiveLoop(node Node) {
	for {
		msg, err := Receive(c.RW.Reader)
		if err != nil {
			fmt.Println("[Receive Loop][ERROR]", c.ID, ": ", err)
			// if !c.tryToReconnect() {
			// 	return
			// }
			// //renew handshake for this connection
			// node.HandShake(c)
			// continue

			return
		}

		if ch, ok := c.pending[msg.ID]; ok {
			ch <- msg
			delete(c.pending, msg.ID)
			continue
		}

		switch msg.Type {
		case HEARTBEAT:
			go node.ReceiveHeartBeat(msg, c)
		default:
			go node.AsyncEvent(msg, c)
		}

		//this can be 100% simplified with some if statments and no switch
		//TODO: use it after refactoring node

		// var nodeB any = NodeBase{}
		// worker, okW := nodeB.(WorkerNode)
		// manager, okM := nodeB.(ManagerNode)

		// if !okW && !okM {
		// 	continue
		// }
		// switch msg.Type {
		// case HEARTBEAT:
		// 	if okM {
		// 		go manager.ReceiveHeartBeat(msg, c)
		// 	}
		// default:
		// 	if okW {
		// 		go worker.AsyncEvent(msg, c)
		// 	}
		// }
	}
}

func (c *Connection) SendRequest(msg Message) (Message, error) {
	if msg.ID == "" {
		msg.ID = crypto.GenerateID(4)
	}

	ch := make(chan Message, 1)

	c.Mu.Lock()
	c.pending[msg.ID] = ch
	fmt.Println("[Send Request]: started pending on key", msg.ID, "and conn(ID-", c.ID, ", ", c.Nc.LocalAddr(), "->", c.Nc.RemoteAddr(), ")")
	c.Mu.Unlock()

	fmt.Println("[Send Request]: full message: ", msg)
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

func (c *Connection) SendRequestChan(msg Message) (<-chan Message, error) {
	if msg.ID == "" {
		msg.ID = crypto.GenerateID(crypto.MESSAGE_ID)
	}

	ch := make(chan Message, 1)

	c.Mu.Lock()
	c.pending[msg.ID] = ch
	fmt.Println("[Send Request Chan]: started pending on key", msg.ID, "and conn(ID-", c.ID, ", ", c.Nc.LocalAddr(), "->", c.Nc.RemoteAddr(), ")")
	c.Mu.Unlock()

	fmt.Println("[Send Request Chan]: full message: ", msg)
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

func (c *Connection) ReceiveLoopChan(node Node) {
	for {
		msg, err := Receive(c.RW.Reader)
		if err != nil {
			fmt.Println("[Receive Loop Chan][ERROR]:", c.ID, ": ", err)
			if !c.tryToReconnect() {
				return
			}

			//renew handshake for this connection
			node.HandShake(c)
			continue
		}

		fmt.Println("Received message:", msg, "by node:", node.String())
		if ch, ok := c.pending[msg.ID]; ok {

			ch <- msg
			if !msg.IsStream {
				c.Mu.Lock()
				delete(c.pending, msg.ID)
				close(ch)
				c.Mu.Unlock()
			}
			continue
		}

		switch msg.Type {
		case HEARTBEAT:
			go node.ReceiveHeartBeat(msg, c)
		default:
			go node.AsyncEvent(msg, c)
		}
	}
}

func (c *Connection) tryToReconnect() bool {
	addr := c.Nc.RemoteAddr().String()

	conn, err := TryToConnect(addr, 10*time.Second)
	if err != nil {
		fmt.Println(err)
		return false
	}
	_ = c.Nc.Close()

	c.Nc = conn.Nc
	c.RW = conn.RW

	fmt.Println("[tryToReconnect]: Restored ID:", c.ID)

	return true
}

// Duration can be passed as pure int, but will be represented in nanoseconds, be explicit about duration time
func TryToConnect(addr string, deadlineDur time.Duration) (*Connection, error) {
	deadline := time.Now().Add(deadlineDur)

	for time.Now().Before(deadline) {
		nc, err := net.DialTimeout(
			"tcp",
			addr,
			500*time.Millisecond,
		)

		if err == nil {
			conn := NewConnection(nc)
			conn.ID = crypto.GenerateID(crypto.CONN)

			fmt.Printf("[TryToConnect]: Connection succeded! To:%v with ID:%v\n", nc.RemoteAddr().String(), conn.ID)

			return conn, nil
		}

		fmt.Println("Connection failed:", err, "- Retring!")
		time.Sleep(400 * time.Millisecond)
	}

	fmt.Println()

	return nil, fmt.Errorf("[TryToConnect]: Connection failed to %v", addr)
}
