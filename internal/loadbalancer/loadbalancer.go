package loadbalancer

import (
	"fmt"
	"net"
	crypto "networking/tcp/internal/crypto"
	json "networking/tcp/internal/json"
	"networking/tcp/internal/logger"
	"networking/tcp/internal/protocol"
	"networking/tcp/internal/utils"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var log logger.Loggers = logger.NewLoggers(
	logger.WithConsole(os.Stdout, os.Stderr),
	logger.WithBaseOptions(
		logger.PrefixField("lbalancer"),
		logger.FormatField(logger.BASE_PREFIX),
	),
)

type LoadBalancer struct {
	controllerConn *protocol.Connection

	msInfo map[protocol.ServiceType][]*protocol.MsInfo
	msConn map[string]*protocol.Connection

	apiConn map[string]*protocol.Connection

	listener net.Listener

	RWmu sync.RWMutex

	currentLoad utils.Counter
}

func NewLoadBalancer(listPort string) (*LoadBalancer, error) {
	listen, err := net.Listen("tcp", listPort)
	if err != nil {
		return nil, err
	}

	return &LoadBalancer{
		listener: listen,
		msInfo:   make(map[protocol.ServiceType][]*protocol.MsInfo),
		msConn:   make(map[string]*protocol.Connection),
		apiConn:  make(map[string]*protocol.Connection),
	}, nil
}

func (lb *LoadBalancer) Start() {
	log["console"].Info("listening on %s", lb.listener.Addr().String())

	for {
		nc, err := lb.listener.Accept()
		if err != nil {
			log["console"].Error("Accept error: %w", err)
			continue
		}

		log["console"].Info("Accepted connection from %s", nc.RemoteAddr().String())
		go lb.handleConnection(nc)
	}
}

func (lb *LoadBalancer) handleConnection(nc net.Conn) {

	conn, isController := lb.registerConnection(nc)
	//error accured or controller connected
	if conn == nil || isController {
		return
	}

	defer conn.Close()

	for {
		request, err := protocol.Receive(conn.RW.Reader)
		if err != nil {
			log["console"].Error("Handle connection: %v", err)
			return
		}

		go lb.handleRequest(request, conn)
	}
}

func (lb *LoadBalancer) registerConnection(nc net.Conn) (*protocol.Connection, bool) {
	conn := protocol.NewConnection(nc)

	request, err := protocol.Receive(conn.RW.Reader)
	if err != nil {
		log["console"].Error("Register connection: %v", err)
		return nil, false
	}

	if request.Type != protocol.HELLO {
		log["console"].Error("Expected handshake received: %v", request)
		return nil, false
	}

	isController := false
	if request.Content == "CONTROLLER" {
		log["console"].Debug("Got controller connection, conn: %v", (conn.Nc.LocalAddr().String() + " -> " + conn.Nc.RemoteAddr().String()))

		go conn.ReceiveLoop(lb)

		go lb.SendHeartBeat(conn)

		lb.controllerConn = conn

		isController = true
	}

	if err := protocol.Send(conn.RW.Writer, protocol.Message{
		ID:      request.ID,
		Type:    request.Type,
		Code:    protocol.SUCCESS,
		Content: lb.String(),
	}); err != nil {
		log["console"].Error("Send handshake: %v", err)
		conn.Close()
	}

	log["console"].Debug("Succesfull handshake: %v", request)

	return conn, isController
}

func (lb *LoadBalancer) handleRequest(request protocol.Message, conn *protocol.Connection) {

	lb.currentLoad.IncValue()
	defer lb.currentLoad.DecValue()

	var response protocol.Message
	var respChan <-chan protocol.Message = nil
	var err error

	switch request.Type {
	//microservice operations
	case protocol.PING:
		fallthrough
	case protocol.IDLE:
		fallthrough
	case protocol.DOWNLOAD:
		fallthrough
	case protocol.UPLOAD:
		//forward message to microservice
		var msType protocol.ServiceType

		msType = protocol.ServiceType(request.Type)

		//take first ms with this type
		services := lb.msInfo[msType]

		callControllerFunc := func(lb *LoadBalancer, request protocol.Message) protocol.Message {
			msType = protocol.ServiceType(request.Type)

			req := protocol.Message{
				ID:      crypto.GenerateID(crypto.MESSAGE_ID),
				Type:    protocol.UPDATE,
				Content: lb.String(),
			}

			resp, err := lb.controllerConn.SendRequest(req)
			if err != nil || !resp.IsCorrect(req.Type) {
				return protocol.Message{
					ID:      request.ID,
					Type:    request.Type,
					Code:    protocol.ERROR,
					Content: err.Error(),
				}
			}

			msMap, err := json.FromJSON[map[protocol.ServiceType][]*protocol.MsInfo](resp.Content)
			if err != nil {
				return protocol.Message{
					ID:      request.ID,
					Type:    request.Type,
					Code:    protocol.ERROR,
					Content: err.Error(),
				}
			}

			existingByID := make(map[string]*protocol.MsInfo)
			for _, mses := range lb.msInfo {
				for _, ms := range mses {
					existingByID[ms.Base.ID] = ms
				}
			}

			var errList []string
			for _, mses := range msMap {
				for _, ms := range mses {
					if _, ok := existingByID[ms.Base.ID]; ok {
						continue
					}

					if err := lb.connectToMicroservice(ms); err != nil {
						errList = append(errList, fmt.Sprintf("Cant connect %v", ms))
					}

				}
			}

			if len(errList) > 0 {
				data, err := json.ToJSON(errList)
				if err != nil {
					data = err.Error()
				}

				return protocol.Message{
					ID:      request.ID,
					Type:    request.Type,
					Code:    protocol.ERROR,
					Content: data,
				}
			}

			return protocol.Message{
				Code: protocol.SUCCESS,
			}
		}

		if len(services) < 1 {
			msgStr := fmt.Sprintf("no services with type %s available", msType)
			log["console"].Error(msgStr)

			resp := callControllerFunc(lb, request)
			if resp.Code != protocol.SUCCESS {
				break
			}

			//refresh lb data
			services = lb.msInfo[msType]
		}

		ms := services[0]

		msConn := lb.msConn[ms.Base.ID]

		if msConn == nil {
			response = protocol.Message{
				ID:      request.ID,
				Type:    request.Type,
				Code:    protocol.ERROR,
				Content: "Connection to ms is nil",
			}
			break
		}

		log["console"].Debug("Sending ms request: %v", request)
		respChan, err = msConn.SendRequestChan(request)

		//response, err = msConn.SendRequest(request)
		if err != nil {
			response = protocol.Message{
				ID:      request.ID,
				Type:    request.Type,
				Code:    protocol.ERROR,
				Content: err.Error(),
			}
		}
		log["console"].Debug("Request was sent successfuly: %v", request)

	default:
		response = protocol.Message{
			ID: request.ID, Type: request.Type, Code: protocol.ERROR, Content: "unknown command: " + string(request.Type),
		}
	}

	if respChan == nil {
		response.ID = request.ID

		conn.RWmu.RLock()
		err := protocol.Send(conn.RW.Writer, response)
		conn.RWmu.RUnlock()

		if err != nil {
			log["console"].Error("Error sending response: %w", err)
			return
		}

	} else {

		for response := range respChan {
			response.ID = request.ID

			conn.RWmu.RLock()
			err := protocol.Send(conn.RW.Writer, response)
			conn.RWmu.RUnlock()

			if err != nil {
				log["console"].Error("Error sending response: %w", err)
				return
			}
		}
	}
}

func (lb *LoadBalancer) SendHeartBeat(conn *protocol.Connection) {

	msg := protocol.Message{
		Type:   protocol.HEARTBEAT,
		Sender: lb.String(),
	}

	for {
		time.Sleep(5 * time.Second)

		msg.ID = crypto.GenerateID(crypto.MESSAGE_ID)
		msg.Content = strconv.FormatUint(uint64(lb.currentLoad.GetValue()), 10)

		if err := protocol.Send(conn.RW.Writer, msg); err != nil {
			log["console"].Error("Error sending heartbeat: %v", err)
		}
	}
}

func parseMsFromMessage(msg protocol.Message) (*protocol.MsInfo, error) {
	if msg.Content == "" {
		err := logger.StrToError(log, "Empty message while parsing ms")
		return nil, err
	}

	log["console"].Debug("message:  %v\n", msg)

	dataSplit := strings.Split(msg.Content, ";")

	expectedSplitCount := 5
	if len(dataSplit) != expectedSplitCount {
		err := logger.StrToError(log, "Expected %d values while parsing ms, got: %v", expectedSplitCount, dataSplit)
		return nil, err
	}

	log["console"].Debug("Ms created from message (%v %v %v %v %v)", dataSplit[0], dataSplit[1], dataSplit[2], dataSplit[3], dataSplit[4])

	return &protocol.MsInfo{
		Base: protocol.NodeBase{
			ID:     dataSplit[0],
			Host:   dataSplit[1],
			Port:   dataSplit[2],
			NodeID: dataSplit[3],
		},
		Type: protocol.ServiceType(dataSplit[4]),
	}, nil
}

func (lb *LoadBalancer) connectToMicroservice(ms *protocol.MsInfo) error {
	address := net.JoinHostPort(ms.Base.Host, ms.Base.Port)

	conn, err := protocol.TryToConnect(address, 10*time.Second)
	if err != nil {
		return fmt.Errorf("timeout - connecting to microservice %s", address)
	}

	go conn.ReceiveLoopChan(lb)

	log["console"].Info("Connected to microservice: %s", address)

	handshakeResp, err := conn.SendRequest(protocol.Message{
		Type:    protocol.HELLO,
		Content: lb.String(),
	})
	//TODO: also check if ms sent you response
	if err != nil {
		return logger.StrToError(log, "Sending handshake to microservice %v", err)
	} else if handshakeResp.Code != protocol.SUCCESS || handshakeResp.Type != protocol.HELLO {
		return logger.StrToError(log, "Response from microservice %v", handshakeResp)
	}

	conn.Mu.Lock()
	lb.msInfo[ms.Type] = append(lb.msInfo[ms.Type], ms)
	lb.msConn[ms.Base.ID] = conn
	conn.Mu.Unlock()

	return nil
}

func (lb *LoadBalancer) handleRecover(request protocol.Message) protocol.Message {

	log["console"].Debug("request handle recover: %v", request)

	services, err := json.FromJSON[map[protocol.ServiceType][]*protocol.MsInfo](request.Content)
	if err != nil {
		return protocol.Message{
			ID:      request.ID,
			Type:    request.Type,
			Code:    protocol.ERROR,
			Content: err.Error(),
		}
	}

	for _, mses := range services {
		for _, ms := range mses {
			if ms.Base.Status == protocol.Unhealthy {
				log["console"].Error("Found ms but status is UNHEALTHY")
				continue
			}

			err := lb.connectToMicroservice(ms)
			if err != nil {
				log["console"].Error("connecting to ms %v: %v", ms, err)
				break
			}

			if ms.Process, err = os.FindProcess(ms.PID); err != nil {
				log["console"].Error("Finding ms process PID %v: %v", ms.PID, err)
				break
			}

			log["console"].Debug("Successfully recovered connection to ms %v", ms)
		}
	}

	return protocol.Message{
		ID:   request.ID,
		Type: request.Type,
		Code: protocol.SUCCESS,
	}
}

// ################################# NODE METHODS #################################
func (lb *LoadBalancer) HandShake(conn *protocol.Connection) {
	msg := protocol.Message{
		ID:      crypto.GenerateID(crypto.MESSAGE_ID),
		Type:    protocol.HELLO,
		Content: lb.String(),
	}

	resp, err := conn.SendRequest(msg)
	if err != nil {
		log["console"].Error("Send handshake: %v", err)
		conn.Close()
		return
	} else if !resp.IsCorrect(protocol.HELLO) {
		log["console"].Error("Received handshake: %v", err)
		return
	}

	log["console"].Debug("HandShake successfully completed - Received: %v", resp)

	//for now only ms conn will apply to this, but cant check it yet (if it is ms)
}

func (lb *LoadBalancer) ReceiveHeartBeat(msg protocol.Message, conn *protocol.Connection) {

}

func (lb *LoadBalancer) AsyncEvent(request protocol.Message, conn *protocol.Connection) {

	var response protocol.Message

	switch request.Type {
	case protocol.RECOVER:
		response = lb.handleRecover(request)
	//TODO: later extend this to parsing if update should add, delete or update ms data  (add - add, del - delete, nothing - just update) as a 6th part in msg
	case protocol.UPDATE:
		//update microservice data
		ms, err := parseMsFromMessage(request)
		if err != nil {
			response = protocol.Message{
				ID:      request.ID,
				Type:    protocol.UPDATE,
				Code:    protocol.ERROR,
				Content: err.Error(),
			}
			break
		}

		conn.RWmu.Lock()
		_, ok := lb.msInfo[ms.Type]
		conn.RWmu.Unlock()

		if ms.Base.Status == protocol.Unhealthy {
			strErr := "Found ms but has status UNHEALTHY"
			log["console"].Error(strErr)
			response = protocol.Message{
				ID:      request.ID,
				Type:    protocol.UPDATE,
				Code:    protocol.ERROR,
				Content: strErr,
			}
			break
		}

		if !ok {

			err := lb.connectToMicroservice(ms)
			if err != nil {
				response = protocol.Message{
					ID:      request.ID,
					Type:    protocol.UPDATE,
					Code:    protocol.ERROR,
					Content: err.Error(),
				}
				break
			}

			log["console"].Debug("Successfully added ms to register %v:%v type:%v\n", ms.Base.Host, ms.Base.Port, ms.Type)
		}

		response = protocol.Message{ID: request.ID, Type: request.Type, Code: protocol.SUCCESS}

	default:
		strErr := "unknown async command: " + string(request.Type) + "\n"
		log["console"].Error(strErr)
		response = protocol.Message{
			ID:      request.ID,
			Type:    request.Type,
			Code:    protocol.ERROR,
			Content: strErr,
		}
	}

	if err := protocol.Send(conn.RW.Writer, response); err != nil {
		log["console"].Error("Error sending response: %w\n", err)
		return
	}

}

func (lb *LoadBalancer) String() string {
	return "LOADBALANCER"
}

// ################################# NODE METHODS #################################
