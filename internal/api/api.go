package api

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"networking/tcp/internal/protocol"
	"sync"
)

//TODO: make logs for api to have info in terminal

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

func NewAPIGateway(port int) (*APIGateway, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}

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
	go controllerWorker(conn, api.requestChannel)

	for {
		client, err := api.listener.Accept()
		if err != nil {
			continue
		}
		go api.handleClient(client)
	}
}

func controllerWorker(conn net.Conn, reqChan chan controllerRequest) {
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	var mu sync.Mutex

	for req := range reqChan {
		go func(r controllerRequest) {
			mu.Lock() // tylko jeden request na raz pisze/odczytuje po TCP
			defer mu.Unlock()

			// send request
			reqBytes, _ := json.Marshal(r.data)
			reqBytes = append(reqBytes, '\n')
			writer.Write(reqBytes)
			writer.Flush()

			// receive response
			line, _ := reader.ReadBytes('\n')
			var resp protocol.Message
			json.Unmarshal(line, &resp)

			// send to client
			r.response <- resp
		}(req)
	}
}

func (api *APIGateway) handleClient(client net.Conn) {
	defer client.Close()

	reader := bufio.NewReader(client)
	writer := bufio.NewWriter(client)

	sessionID := generateSessionID()

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

		request.ID = sessionID

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

func generateSessionID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// -------------------- FUNCTIONS -----------------------
