package microservice

import (
	"bufio"
	"net"
	"networking/tcp/internal/logger"
	"networking/tcp/internal/protocol"
	"os"
	"time"
)

var log logger.Loggers = logger.InitLoggers(
	logger.WithConsole(os.Stdout, os.Stderr),
	logger.WithBaseOptions(
		logger.PrefixField("service"),
		logger.FormatField(logger.BASE_PREFIX),
	),
)

type Microservice struct {
	listener net.Listener
}

func NewMicroservice(listenerPort string) (*Microservice, error) {
	listen, err := net.Listen("tcp", listenerPort)
	if err != nil {
		return nil, err
	}

	return &Microservice{
		listener: listen,
	}, nil
}

func (ms *Microservice) Start(serviceType string) {
	log["console"].Info("service %s started on %s\n", serviceType, ms.listener.Addr())

	switch serviceType {
	case "ping":
		ms.acceptConnections(ms.pingService)
	default:
		log["console"].Error("unknow service type %s", serviceType)
	}
}

func (ms *Microservice) acceptConnections(serviceFunc func(conn net.Conn)) {
	for {
		conn, err := ms.listener.Accept()
		if err != nil {
			log["console"].Error("accepting connection error %w", err)
			return
		}

		log["console"].Info("accepted connection")

		go serviceFunc(conn)
	}
}

func (ms *Microservice) pingService(conn net.Conn) {
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for {
		request, err := protocol.Receive(reader)
		if err != nil {
			log["console"].Error("reading request error %w", err)
			return
		}

		log["console"].Debug("received request: %s", request)

		curr_time := time.Now()

		response := protocol.Message{
			SessionID:    request.SessionID,
			ConnectionID: request.ConnectionID,
			Type:         request.Type,
			Code:         protocol.SUCCESS,
			Content:      curr_time.Format("2006-01-02 15:04:05"),
		}

		err = protocol.Send(writer, response)
		if err != nil {
			log["console"].Error("sending response error %w", err)
			return
		}
	}
}

func (ms *Microservice) loginService() {

}
