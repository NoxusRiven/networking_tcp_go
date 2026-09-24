package microservice

import "networking/tcp/internal/protocol"

//fields that all services should have
type ServiceFields struct {
	Parent *Microservice
}

type Service interface {
	HandleRequest(request protocol.Message)
}
