package microservice

import (
	"bufio"
	"fmt"
	"net"
	"networking/tcp/internal/protocol"
	"strings"
	"time"
)

type PingService struct{}
type IdleService struct{}
type LoginSerice struct{}

func (ping *PingService) Work(nc net.Conn) {
	reader := bufio.NewReader(nc)
	writer := bufio.NewWriter(nc)

	for {
		request, err := protocol.Receive(reader)
		if err != nil {
			log["console"].Error("reading request error %w", err)
			return
		}

		log["console"].Debug("received request: %s", request)

		var response protocol.Message

		if strings.ToLower(string(request.Type)) != "ping" {
			errStr := fmt.Sprintf("Wrong request message %v", request.Type)

			log["console"].Error(errStr)

			response = protocol.Message{
				ID:           request.ID,
				SessionID:    request.SessionID,
				ConnectionID: request.ConnectionID,
				Type:         request.Type,
				Code:         protocol.ERROR,
				Content:      errStr,
			}

		} else {
			curr_time := time.Now()

			response = protocol.Message{
				ID:           request.ID,
				SessionID:    request.SessionID,
				ConnectionID: request.ConnectionID,
				Type:         request.Type,
				Code:         protocol.SUCCESS,
				Content:      curr_time.Format("2006-01-02 15:04:05"),
			}
		}

		err = protocol.Send(writer, response)
		if err != nil {
			log["console"].Error("sending response error %w", err)
			return
		}
	}
}

func (idle *IdleService) Work(nc net.Conn) {

}

func (login *LoginSerice) Work(nc net.Conn) {

}
