package microservice

import (
	"bufio"
	"fmt"
	"net"
	"networking/tcp/internal/logger"
	"networking/tcp/internal/protocol"
	"os"
	"strings"
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
	RW *bufio.ReadWriter

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

		//use go rutine to handle every connection async
		go ms.Work(nc)
	}
}

func (ms *Microservice) Work(nc net.Conn) {
	reader := bufio.NewReader(nc)
	writer := bufio.NewWriter(nc)

	ms.RW = bufio.NewReadWriter(reader, writer)

	for {
		request, err := protocol.Receive(ms.RW.Reader)
		if err != nil {
			log["console"].Error("reading request error %w", err)
			return
		}

		log["console"].Debug("received request: %s", request)

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

			if err := protocol.Send(ms.RW.Writer, response); err != nil {
				log["console"].Error("Error while sending response: %v", err)
			}

		} else {
			go ms.Service.HandleRequest(request)
		}

		//message that work has ended
	}
}
