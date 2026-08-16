package microservice

import (
	"net"
	"networking/tcp/internal/logger"
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
	listener net.Listener
	Service  Service
}

func NewMicroservice(listenerPort string) (*Microservice, error) {
	listen, err := net.Listen("tcp", listenerPort)
	if err != nil {
		return nil, err
	}

	return &Microservice{
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

		log["console"].Info("accepted connection")

		ms.Service.Work(nc)
	}
}
