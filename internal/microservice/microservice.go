package microservice

import (
	"bufio"
	"fmt"
	"net"
	crypto "networking/tcp/internal/cryptography"
	"networking/tcp/internal/logger"
	"networking/tcp/internal/protocol"
	"os"
	"strings"
	"time"
)

var log logger.Loggers = logger.NewLoggers(
	logger.WithConsole(os.Stdout, os.Stderr),
	logger.WithBaseOptions(
		logger.PrefixField("service"),
		logger.FormatField(logger.BASE_PREFIX),
	),
)

type Microservice struct {
	//TODO?: maybe later change this to Connection or list of them
	RW        *bufio.ReadWriter
	LbConn    *protocol.Connection
	agentConn *protocol.Connection

	listener net.Listener
	Service  Service
}

func NewMicroservice(listenerPort string) (*Microservice, error) {
	listen, err := net.Listen("tcp", listenerPort)
	if err != nil {
		return nil, err
	}

	return &Microservice{
		RW:       nil,
		listener: listen,
		Service:  nil,
	}, nil
}

func (ms *Microservice) Start(serviceType string) {
	serviceType = strings.ToLower(serviceType)

	if ms.Service == nil {
		log["console"].Error("Unknow service %v\nMicroservice Service field has to be set in main", serviceType)
		return
	}

	log["console"].Info("service %s started on %s\n", serviceType, ms.listener.Addr())

	ms.acceptConnections()
}

func (ms *Microservice) acceptConnections() {
	for {
		nc, err := ms.listener.Accept()
		if err != nil {
			log["console"].Error("accepting connection error %w", err)
			return
		}

		log["console"].Info("accepted new connection")

		//use gorutine to handle every connection async
		go ms.handleConnection(nc)
	}
}

func (ms *Microservice) handleConnection(nc net.Conn) {
	conn := protocol.NewConnection(nc)
	conn.ID = crypto.GenerateID(crypto.CONN)

	msg, err := protocol.Receive(conn.RW.Reader)
	if err != nil {
		log["console"].Error("Error while receiving message from connection: %v", err)
		return
	} else if msg.Type != protocol.HELLO {
		log["console"].Error("Wrong message type during handshake: %v", msg)
		return
	}

	log["console"].Debug("got message %v", msg)

	err = protocol.Send(conn.RW.Writer, protocol.Message{
		ID:   msg.ID,
		Type: msg.Type,
		Code: protocol.SUCCESS,
	})
	if err != nil {
		log["console"].Error("handle Connection Send: %v", err)
		return
	}

	if msg.Content == "AGENT" {
		log["console"].Debug("Found agent conn")
		ms.agentConn = conn
		go ms.SendHeartBeat(conn)
	} else {
		ms.LbConn = conn
		go ms.Work(conn)
	}
}

func (ms *Microservice) Work(conn *protocol.Connection) {
	for {
		request, err := protocol.Receive(ms.LbConn.RW.Reader)
		if err != nil {
			log["console"].Error("reading request error %v", err)
			return
		}

		log["console"].Debug("Succesfully received message %v", request)

		if strings.ToLower(string(request.Type)) != String(ms.Service) {
			errStr := fmt.Sprintf("Wrong request message %v", request.Type)

			log["console"].Error(errStr)

			response := protocol.Message{
				ID:           request.ID,
				SessionID:    request.SessionID,
				ConnectionID: request.ConnectionID,
				Type:         request.Type,
				Code:         protocol.ERROR,
				Content:      errStr,
			}

			if err := protocol.Send(ms.LbConn.RW.Writer, response); err != nil {
				log["console"].Error("Error while sending response: %v", err)
			}

		} else {
			go ms.Service.HandleRequest(request)
		}

		//message that work has ended
	}
}

func (ms *Microservice) SendHeartBeat(conn *protocol.Connection) {
	msg := protocol.Message{
		Type: protocol.HEARTBEAT,
	}

	for {
		time.Sleep(5 * time.Second)

		msg.ID = crypto.GenerateID(crypto.MESSAGE_ID)
		if err := protocol.Send(conn.RW.Writer, msg); err != nil {
			log["console"].Error("Error accured when trying to send heart beat to agent %v", err)
			break
		}
	}
}
