package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	crypto "networking/tcp/internal/cryptography"
	"networking/tcp/internal/protocol"
)

/**
*TODO: make api directly connect to lbs and do inteligent loadbalancing loadbalancers (prolly by load)
 */

const (
	CONNECTION_NUM = 4
)

// -------------------- STRUCTURES -----------------------

type controllerRequest struct {
	data     protocol.Message
	response chan protocol.Message
}

type APIGateway struct {
	listener       net.Listener
	requestChannel chan controllerRequest
}

// -------------------- STRUCTURES -----------------------

// -------------------- FUNCTIONS -----------------------

func NewAPIGateway(port uint32) (*APIGateway, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}

	fmt.Println("API Gateway listining on [::]:", port)

	return &APIGateway{
		listener: ln,
	}, nil
}

func (api *APIGateway) Start(controllerAddr string) error {

	api.requestChannel = make(chan controllerRequest)

	// start worker
	for i := 0; i < CONNECTION_NUM; i++ {
		conn, err := net.Dial("tcp", controllerAddr)
		if err != nil {
			fmt.Println("Error connecting to controller:", err)
			continue
		}

		rw, err := registerConn(conn)
		if err != nil {
			continue
		}

		go controllerWorker(rw, api.requestChannel)
	}

	for {
		client, err := api.listener.Accept()
		if err != nil {
			continue
		}
		fmt.Println("New Client accepted")
		go api.handleClient(client)
	}
}

func registerConn(conn net.Conn) (*bufio.ReadWriter, error) {
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	err := protocol.Send(w, protocol.Message{
		Type: protocol.REG_API,
	})
	if err != nil {
		return nil, err
	}

	resp, err := protocol.Receive(r)
	if err != nil {
		return nil, err
	}

	if resp.Code != protocol.SUCCESS {
		return nil, fmt.Errorf("Register conn err: %s", resp.Content)
	}

	return bufio.NewReadWriter(r, w), nil
}

func controllerWorker(rw *bufio.ReadWriter, reqChan chan controllerRequest) {

	for req := range reqChan {

		// send request
		protocol.Send(rw.Writer, req.data)
		fmt.Println("Request sent to Controller:", req.data)

		// receive response
		resp, err := protocol.Receive(rw.Reader)
		if err != nil {
			fmt.Println("controller worker:", err)
			return
		}

		// send to client
		req.response <- resp
		fmt.Println("Response from Controller:", resp)
	}
}

func (api *APIGateway) handleClient(client net.Conn) {
	defer client.Close()

	reader := bufio.NewReader(client)
	writer := bufio.NewWriter(client)

	sessionID := crypto.GenerateID()

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}

		var request protocol.Message

		err = json.Unmarshal(line, &request)
		if err != nil {
			fmt.Println("Invalid JSON:", err)
			continue
		}

		request.SessionID = sessionID

		respChan := make(chan protocol.Message)

		// sent request to worker
		api.requestChannel <- controllerRequest{
			data:     request,
			response: respChan,
		}

		// wait for response
		response := <-respChan

		respBytes, err := json.Marshal(response)
		if err != nil {
			fmt.Println("JSON encode error:", err)
			continue
		}

		respBytes = append(respBytes, '\n')

		_, err = writer.Write(respBytes)
		if err != nil {
			return
		}
		writer.Flush()
	}
}

// -------------------- FUNCTIONS -----------------------
