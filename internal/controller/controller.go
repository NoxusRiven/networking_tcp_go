package controller

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"networking/tcp/internal/protocol"
)

//TODO: for some reason "Client with id requested exit" only works for first client not others even tho sessions and sending by many is fine

// One api connection
type Controller struct {
	apiListener net.Listener
}

func NewController(apiPort int) (*Controller, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", apiPort))
	if err != nil {
		return nil, err
	}

	return &Controller{
		apiListener: listener,
	}, nil
}

// starts and accepts api connections
func (c *Controller) Start() error {
	fmt.Println("Controller listening for API on", c.apiListener.Addr().String())

	for {
		conn, err := c.apiListener.Accept()
		if err != nil {
			fmt.Println("Accept error:", err)
			continue
		}

		fmt.Println("Accepted new API connection")
		// jedno połączenie do API Gateway
		go c.handleAPIConnection(conn)
	}
}

// one tcp connection many requests handled
func (c *Controller) handleAPIConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// iterate thourugh requests
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			fmt.Println("API Gateway disconnected")
			return
		}

		// reeading message
		var request protocol.Message
		err = json.Unmarshal(line, &request)
		if err != nil {
			fmt.Println("Invalid JSON:", err)
			continue
		}

		fmt.Printf("Received request: ID=%s, Type=%s, Content=%s\n",
			request.ID, request.Type, request.Content)

		// message handling
		var response protocol.Message
		switch request.Type {
		case protocol.EXIT:
			fmt.Printf("Client with sessionID %s requested exit\n", request.ID)
		case protocol.PING:
			response = protocol.Message{
				ID:      request.ID,
				Type:    request.Type,
				Code:    protocol.SUCCESS,
				Content: "PONG from Controller",
			}
		default:
			response = protocol.Message{
				ID:      request.ID,
				Type:    request.Type,
				Code:    protocol.SUCCESS,
				Content: "Message from Controller",
			}
		}

		respBytes, err := json.Marshal(response)
		if err != nil {
			fmt.Println("JSON encode error:", err)
			return
		}
		respBytes = append(respBytes, '\n')
		_, err = writer.Write(respBytes)
		if err != nil {
			fmt.Println("Write error:", err)
			return
		}
		writer.Flush()
	}
}
