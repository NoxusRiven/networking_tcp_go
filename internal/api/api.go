package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	crypto "networking/tcp/internal/cryptography"
	"networking/tcp/internal/protocol"
)

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

	conn, err := net.Dial("tcp", controllerAddr)
	if err != nil {
		return err
	}

	api.requestChannel = make(chan controllerRequest)

	// start worker
	for i := 0; i < CONNECTION_NUM; i++ {
		go controllerWorker(conn, api.requestChannel)
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

func controllerWorker(conn net.Conn, reqChan chan controllerRequest) {
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for req := range reqChan {

		// send request
		reqBytes, _ := json.Marshal(req.data)
		reqBytes = append(reqBytes, '\n')
		writer.Write(reqBytes)
		writer.Flush()
		fmt.Println("Request sent to Controller:", req.data)

		// receive response
		line, _ := reader.ReadBytes('\n')
		var resp protocol.Message
		json.Unmarshal(line, &resp)

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
