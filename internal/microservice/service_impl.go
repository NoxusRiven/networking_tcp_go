package microservice

import (
	"networking/tcp/internal/protocol"
	"time"
)

type PingService struct{ Fields ServiceFields }
type IdleService struct{ Fields ServiceFields }

func String(s Service) string {
	switch s.(type) {
	case *PingService:
		return "ping"
	case *IdleService:
		return "idle"
	default:
		return "none"
	}
}

//type LoginSerice struct{}

func (ping *PingService) HandleRequest(request protocol.Message) {
	curr_time := time.Now()

	response := protocol.Message{
		ID:           request.ID,
		SessionID:    request.SessionID,
		ConnectionID: request.ConnectionID,
		Type:         request.Type,
		Code:         protocol.SUCCESS,
		Content:      curr_time.Format("2006-01-02 15:04:05"),
	}

	log["console"].Debug("sending response to lb %v", response)
	err := protocol.Send(ping.Fields.Parent.RW.Writer, response)
	if err != nil {
		log["console"].Error("sending response error %w", err)
		return
	}
}

func (idle *IdleService) HandleRequest(request protocol.Message) {
	//for now pre defined
	n := 10

	//sending work n times
	for i := 0; i < n; i++ {
		response := protocol.Message{
			ID:           request.ID,
			SessionID:    request.SessionID,
			ConnectionID: request.ConnectionID,
			Type:         request.Type,
			IsStream:     true,
			Code:         protocol.SUCCESS,
			Content:      i,
		}

		err := protocol.Send(idle.Fields.Parent.RW.Writer, response)
		if err != nil {
			log["console"].Error("sending response error %w", err)
			return
		}

		time.Sleep(time.Duration(2 * time.Second))
	}

	response := protocol.Message{
		ID:           request.ID,
		SessionID:    request.SessionID,
		ConnectionID: request.ConnectionID,
		Type:         request.Type,
		Code:         protocol.SUCCESS,
	}

	log["console"].Debug("sending response to lb %v", response)
	err := protocol.Send(idle.Fields.Parent.RW.Writer, response)
	if err != nil {
		log["console"].Error("sending response error %w", err)
		return
	}
}

// func (login *LoginSerice) Work(nc net.Conn) {

// }
