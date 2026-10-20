package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	crypto "networking/tcp/internal/cryptography"
	"networking/tcp/internal/logger"
	"networking/tcp/internal/protocol"
	"os"
)

var log = logger.NewLoggers(
	logger.WithConsole(os.Stderr, os.Stderr),
	logger.WithBaseOptions(
		logger.PrefixField("api"),
		logger.FormatField(logger.BASE_PREFIX),
	),
)

/**
*TODO: make api do inteligent loadbalancing loadbalancers (prolly by load)
 */

const (
	CONNECTION_NUM = 4
)

// ##################################### STRUCTURES #####################################

type MessageExchange struct {
	request  protocol.Message
	response chan protocol.Message
}

type APIGateway struct {
	listener                 net.Listener
	controllerRequestChannel chan *MessageExchange

	controllerConn *protocol.Connection

	// type - list of loadbalancers
	lbInfo map[string]*protocol.LBalancerInfo
	// lb id - connection num of connections to lb (for performance reasons)
	lbConn map[string][]*protocol.Connection

	requestChannelMap map[string]chan *MessageExchange
}

// ##################################### STRUCTURES #####################################

// ################################## CORE #####################################

func NewAPIGateway(port uint32) (*APIGateway, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}

	log["console"].Info("API Gateway listining on [::]: %d", port)

	return &APIGateway{
		listener: ln,
		lbInfo:   make(map[string]*protocol.LBalancerInfo),
		lbConn:   make(map[string][]*protocol.Connection),
	}, nil
}

func (api *APIGateway) Start(controllerAddr string) error {

	api.controllerRequestChannel = make(chan *MessageExchange)

	api.requestChannelMap = make(map[string]chan *MessageExchange)

	nc, err := net.Dial("tcp", controllerAddr)
	if err != nil {
		return err
	}

	api.controllerConn = protocol.NewConnection(nc)
	defer api.controllerConn.Close()

	go api.controllerConn.ReceiveLoopChan(api)

	err = api.RegisterToController(api.controllerConn)
	if err != nil {
		return err
	}

	for {
		clientNC, err := api.listener.Accept()
		if err != nil {
			continue
		}

		cliConn := protocol.NewConnection(clientNC)

		cliConn.ID = crypto.GenerateID(crypto.CONN)

		fmt.Println("New Client accepted")
		go api.handleClient(cliConn)
	}
}

func (api *APIGateway) RegisterToController(conn *protocol.Connection) error {

	respChan, err := conn.SendRequestChan(protocol.Message{
		Type:    protocol.HELLO,
		Content: api.String(),
	})
	respHello := <-respChan

	if err != nil {
		return err
	}
	if respHello.Type != protocol.HELLO || respHello.Code != protocol.SUCCESS || respHello.Content != "CONTROLLER" {
		return logger.StrToErrorNew(log, "Controller responsed with %v during handshake with api", respHello)
	}

	log["console"].Debug("Registered succesfully %v", respHello)

	respSync := <-respChan

	// first sync will happend after registration, every other will happen asynchronously
	err = api.syncLoadBalancers(respSync)
	if err != nil {
		return err
	}

	log["console"].Debug("Sync response from controller %v", respSync)

	return nil
}

func (api *APIGateway) syncLoadBalancers(msg protocol.Message) error {

	var lbList []*protocol.LBalancerInfo

	bytes, err := json.Marshal(msg.Content)
	if err != nil {
		return logger.StrToErrorNew(log, "Unable to marshal loadbalancer info, error: %v", err)
	}

	err = json.Unmarshal(bytes, &lbList)
	if err != nil {
		return logger.StrToErrorNew(log, "Unable to decode loadbalancers from controllers message, error: %v", err)
	}

	log["console"].Debug("Converted json into lb list")
	for _, lb := range lbList {
		log["console"].Debug("LB: %s:%s, ID: %s", lb.Host, lb.Port, lb.ID)
	}

	// for every loadbalancer start CONNECTION_NUM of connections
	for _, lb := range lbList {

		//if connections exists, dont create new MsgExchnge and delete previous data
		conns, okConn := api.lbConn[lb.ID]
		var conn *protocol.Connection
		if !okConn {
			log["console"].Debug("creating new MsgExchnge")
			api.requestChannelMap[lb.ID] = make(chan *MessageExchange, CONNECTION_NUM)
		}

		for i := 0; i < CONNECTION_NUM; i++ {

			//TODO? maybe isolate this logic into method
			nc, err := net.Dial("tcp", net.JoinHostPort(lb.Host, lb.Port))
			if err != nil {
				log["console"].Error("Error connecting to loadbalancer %s:%s, error: %v", lb.Host, lb.Port, err)
				continue
			}

			if !okConn {
				conn = protocol.NewConnection(nc)
				conn.ID = crypto.GenerateID(crypto.CONN)

			} else {
				//Just swap net.Conn, meta Connection data remains the same
				conn = conns[i]
				conn.Nc.Close()

				conn.Nc = nc
				RW := bufio.NewReadWriter(
					bufio.NewReader(nc),
					bufio.NewWriter(nc),
				)
				conn.RW = RW
			}

			go conn.ReceiveLoopChan(api)

			//? maybe if conn to lb isnt working return and mark it as unhealty, and dont check other connection (also send info to controller about it)

			//test if every connection works
			respChan, err := conn.SendRequestChan(protocol.Message{
				Type:    protocol.HELLO,
				Content: api.String(),
			})

			if err != nil {
				log["console"].Error("Error while sending request %v", err)
				continue
			}
			resp, ok := <-respChan

			if !ok {
				log["console"].Error("Channel closed")
				continue
			}

			if resp.Code != protocol.SUCCESS {
				log["console"].Error("Loadbalancer %s:%s responsed with error code %d to heartbeat on %s connID, response: %v", lb.Host, lb.Port, resp.Code, conn.ID, resp)
				continue
			}

			//successfully connected to lb, now map them to thier data structures
			log["console"].Debug("Response from loadbalancer %s:%s on conn(ID-%s, LocAddr-%s): %v", lb.Host, lb.Port, conn.ID, conn.Nc.LocalAddr(), resp)

			if !okConn {
				api.lbInfo[conn.ID] = lb
				api.lbConn[lb.ID] = append(api.lbConn[lb.ID], conn)
			}

			go api.messageWorker(conn)
		}
	}

	return nil
}

func (api *APIGateway) messageWorker(conn *protocol.Connection) {
	lb := api.lbInfo[conn.ID]

	//var err error
	for msgEx := range api.requestChannelMap[lb.ID] {
		_, ok := api.lbInfo[conn.ID]
		if !ok {
			log["console"].Debug("None existant connectionconn =  - stopping messageWroker\n")
			return
		}

		req := msgEx.request
		log["console"].Debug("Message worker got request: %v", req)

		respChan, err := conn.SendRequestChan(req)

		if err != nil {
			log["console"].Error("Error receiving message channel from loadbalancer on connID %s, error: %v", conn.ID, err)
			return
		}

		go func(msgEx *MessageExchange, respChan <-chan protocol.Message) {
			for resp := range respChan {
				log["console"].Debug("Message worker forwarding response: %v", resp)

				msgEx.response <- resp
			}
		}(msgEx, respChan)
	}
}

func (api *APIGateway) handleClient(cliConn *protocol.Connection) {
	defer cliConn.Close()

	for {
		request, err := protocol.Receive(cliConn.RW.Reader)
		if err != nil {
			log["console"].Error("Error while reading request from client: %v", err)
			return
		}

		log["console"].Debug("Received request from client: %v", request)

		if request.Type == "EXIT" {
			log["console"].Info("Client disconnected")
			return
		}

		reqPool := &MessageExchange{
			request:  request,
			response: make(chan protocol.Message, 8),
		}

		lb := api.findLbForRequest(request)
		if lb == nil {
			return
		}

		api.requestChannelMap[lb.ID] <- reqPool

		for response := range reqPool.response {
			response.ID = request.ID

			log["console"].Debug("trying to send response to client %v", response)

			cliConn.Mu.Lock()
			err = protocol.Send(cliConn.RW.Writer, response)
			cliConn.Mu.Unlock()

			if err != nil {
				log["console"].Error("Error while sending response to client: %v", err)
				return
			}

			log["console"].Debug("Sent response to client: %v", response)

			if !response.IsStream {
				close(reqPool.response)
			}
		}
	}

}

func (api *APIGateway) findLbForRequest(request protocol.Message) *protocol.LBalancerInfo {
	//TODO: later make better logic for choosing lb

	log["console"].Debug("Request type str: %v", string(request.Type))
	var lb *protocol.LBalancerInfo
	for _, lbInf := range api.lbInfo {
		if _, ok := lbInf.Microservices[protocol.ServiceType(request.Type)]; ok {
			lb = lbInf
			break
		}
	}

	log["console"].Debug("Found lb :%v", lb)

	if lb == nil {
		respChan, err := api.controllerConn.SendRequestChan(protocol.Message{
			Type:    protocol.CREATE,
			Content: string(request.Type),
		})
		resp := <-respChan

		log["console"].Debug("Response from controller after asking to create new lb for request: %v", resp)

		if err != nil {
			log["console"].Error("Error while sending request to controller to create %v ms for request: %v", request.Content, err)
			return nil
		}

		//api.update
		if resp.Type != protocol.UPDATE {
			log["console"].Error("Controller responsed with type %d to  request for creating %v ms, response: %v", resp.Type, request.Content, resp)
			return nil
		}

		lbUpdate := &protocol.LBalancerInfo{}
		bytes, err := json.Marshal(resp.Content)
		if err != nil {
			log["console"].Error("Unable to marshal loadbalancer info from controller response, error: %v", err)
			return nil
		}

		err = json.Unmarshal(bytes, &lbUpdate)
		if err != nil {
			log["console"].Error("Unable to decode loadbalancers from controller response, error: %v", err)
			return nil
		}

		//? check if double map lookup is faster then looping through full map

		lbconn := api.lbConn[lbUpdate.ID][0]
		lbfound := api.lbInfo[lbconn.ID]

		log["console"].Debug("lb microservices: %v", lbUpdate.Microservices)

		*lbfound = *lbUpdate
		lb = lbfound
	}

	return lb

}

// ##################################### CORE #####################################

// ################################# NODE METHODS #################################
func (api *APIGateway) HandShake(conn *protocol.Connection) {
	//api.lbInfo[conn.ID] = lb
	//api.lbConn[lb.ID] = append(api.lbConn[lb.ID], conn)

	go conn.ReceiveLoop(api)
	go api.messageWorker(conn)
}

func (api *APIGateway) ReceiveHeartBeat(msg protocol.Message, conn *protocol.Connection) {

}

func (api *APIGateway) AsyncEvent(msg protocol.Message, conn *protocol.Connection) {
	switch msg.Type {
	case protocol.UPDATE:
		log["console"].Debug("Got update async event")
		if err := api.syncLoadBalancers(msg); err != nil {
			log["console"].Error("Async event: ", err)
		}
	default:
		//log["console"].Debug("Forwarding to Client %v", msg)

		log["console"].Error("Unsuported async msg: %v", msg)
	}
}

func (api *APIGateway) String() string {
	return "API"
}

// ################################# NODE METHODS #################################
