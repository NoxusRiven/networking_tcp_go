package microservice

import "net"

type Service interface {
	Work(nc net.Conn)
}
