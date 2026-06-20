package api

import (
	"encoding/json"
	"fmt"
	"net"
	crypto "networking/tcp/internal/cryptography"
	"networking/tcp/internal/logger"
	"networking/tcp/internal/protocol"
	"os"
	"time"
)

var log = logger.NewLoggers(
	logger.WithConsole(os.Stderr, os.Stderr),
	logger.WithBaseOptions(
		logger.PrefixField("api"),
		logger.FormatField(logger.BASE_PREFIX),
	),
)

/**
*TODO: make api directly connect to lbs and do inteligent loadbalancing loadbalancers (prolly by load)
 */

const (
	CONNECTION_NUM = 4
)

// -------------------- STRUCTURES -----------------------

type controllerRequest struct {
	data     protocol.Message
	response chan protocol.Message
}

type MessagePool struct {
	data     protocol.Message
	response chan protocol.Message
}

type APIGateway struct {
	listener                 net.Listener
	controllerRequestChannel chan controllerRequest

	controllerConn *protocol.Connection

	// type - list of loadbalancers
	lbInfo map[string]*protocol.LBalancerInfo
	// lb id - connection num of connections to lb (for performance reasons)
	lbConn map[string][]*protocol.Connection

	requestChannelMap map[string]chan MessagePool
}

// -------------------- STRUCTURES -----------------------

// -------------------- FUNCTIONS -----------------------

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

	api.controllerRequestChannel = make(chan controllerRequest)

	api.requestChannelMap = make(map[string]chan MessagePool)

	nc, err := net.Dial("tcp", controllerAddr)
	if err != nil {
		return err
	}

	api.controllerConn = protocol.NewConnection(nc)
	defer api.controllerConn.Close()

	go api.controllerConn.ReceiveLoop(api)

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

	//select {}
}

func (api *APIGateway) RegisterToController(conn *protocol.Connection) error {

	resp, err := conn.SendRequest(protocol.Message{
		Type: protocol.REG_API,
	})

	if err != nil {
		return err
	}
	if resp.Code != protocol.SUCCESS {
		return logger.StrToErrorNew(log["string"], fmt.Sprintf("Controller responsed with error code %d when registering api", resp.Code))
	}

	// first sync will happend after registration, every other will happen asynchronously
	err = api.syncLoadBalancers(resp)
	if err != nil {
		return err
	}

	log["console"].Debug("Response from controller after registering %s", resp)

	return nil
}

func (api *APIGateway) syncLoadBalancers(msg protocol.Message) error {
	//TODO: make loadbalancer info from controller message
	var lbList []*protocol.LBalancerInfo

	bytes, err := json.Marshal(msg.Content)
	if err != nil {
		return logger.StrToErrorNew(log["console"], fmt.Sprintf("Unable to marshal loadbalancer info, error: %v", err))
	}

	err = json.Unmarshal(bytes, &lbList)
	if err != nil {
		return logger.StrToErrorNew(log["console"], fmt.Sprintf("Unable to decode loadbalancers from controllers message, error: %v", err))
	}

	log["console"].Debug("Converted json into lb list")
	for _, lb := range lbList {
		log["console"].Debug("LB: %s:%s, ID: %s", lb.Host, lb.Port, lb.ID)
	}

	// for every loadbalancer start CONNECTION_NUM of connections
	for _, lb := range lbList {
		api.requestChannelMap[lb.ID] = make(chan MessagePool, CONNECTION_NUM)

		for i := 0; i < CONNECTION_NUM; i++ {

			//TODO? maybe isolate this logic into method

			nc, err := net.Dial("tcp", net.JoinHostPort(lb.Host, lb.Port))
			if err != nil {
				log["console"].Error("Error connecting to loadbalancer %s:%s, error: %v", lb.Host, lb.Port, err)
				continue
			}

			conn := protocol.NewConnection(nc)

			//test if every connection works
			protocol.Send(conn.RW.Writer, protocol.Message{
				Type: protocol.HEARTBEAT,
			})

			//? maybe if conn to lb isnt working return and mark it as unhealty, and dont check other connection (also send info to controller about it)
			resp, err := protocol.Receive(conn.RW.Reader)
			if err != nil {
				log["console"].Error("Error receiving response from loadbalancer %s:%s on %s connID, error: %v", lb.Host, lb.Port, conn.ID, err)

				continue
			}
			if resp.Code != protocol.SUCCESS {
				log["console"].Error("Loadbalancer %s:%s responsed with error code %d to heartbeat on %s connID, response: %v", lb.Host, lb.Port, resp.Code, conn.ID, resp)
				continue
			}

			log["console"].Debug("Response from loadbalancer %s:%s on %s connID: %v", lb.Host, lb.Port, conn.ID, resp)

			lb.Mu.Lock()
			lb.LastHeartbeat = time.Now()
			lb.Status = protocol.Healthy
			lb.Mu.Unlock()

			//successfully connected to lb, now map them to thier data structures
			conn.ID = crypto.GenerateID(crypto.CONN)

			api.lbInfo[conn.ID] = lb
			api.lbConn[lb.ID] = append(api.lbConn[lb.ID], conn)

			go api.messageWorker(conn)
		}
	}

	return nil
}

// TODO: later separate sending and receiving logic for different gorutines and match them with pending[id] (propably use conn.ReceiveLoop and SendRequest for this)
func (api *APIGateway) messageWorker(conn *protocol.Connection) {
	lb := api.lbInfo[conn.ID]

	for pool := range api.requestChannelMap[lb.ID] {
		req := pool.data
		log["console"].Debug("Message worker got request: %v", req)
		protocol.Send(conn.RW.Writer, req)

		resp, err := protocol.Receive(conn.RW.Reader)
		if err != nil {
			log["console"].Error("Error receiving response from loadbalancer on %s connID, error: %v", conn.ID, err)
			continue
		}
		if resp.Code != protocol.SUCCESS {
			log["console"].Error("Loadbalancer responsed with error code %d to request on %s connID, response: %v", resp.Code, conn.ID, resp)
			continue
		}

		pool.response <- resp
		log["console"].Debug("Message worker got response: %v", resp)
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

		reqPool := MessagePool{
			data:     request,
			response: make(chan protocol.Message, 1),
		}

		lb := api.findLbForRequest(request)

		api.requestChannelMap[lb.ID] <- reqPool

		response := <-reqPool.response

		err = protocol.Send(cliConn.RW.Writer, response)
		if err != nil {
			log["console"].Error("Error while sending response to client: %v", err)
			return
		}

		log["console"].Debug("Sent response to client: %v", response)
	}

}

func (api *APIGateway) findLbForRequest(request protocol.Message) *protocol.LBalancerInfo {
	//TODO: later make better logic for choosing lb

	var lb *protocol.LBalancerInfo
	for _, lbInf := range api.lbInfo {
		if _, ok := lbInf.Microservices[string(request.Type)]; ok {
			lb = lbInf
			break
		}
	}

	//TODO: fix parsing new loadbalancer and fix communicating to it ping message
	if lb == nil {
		resp, err := api.controllerConn.SendRequest(protocol.Message{
			Type:    protocol.CREATE,
			Content: string(request.Type),
		})

		log["console"].Debug("Response from controller after asking to create new lb for request: %v", resp)

		if err != nil {
			log["console"].Error("Error while sending request to controller to create %v ms for request: %v", request.Content, err)
			return nil
		}
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

		*lbfound = *lbUpdate
		lb = lbfound
	}

	return lb

}

// func (api *APIGateway) handleClient(client *protocol.Connection) {
// 	defer client.Close()

// 	//TODO: change logic to use connection send and receive loop to comunicate with clients

// 	sessionID := crypto.GenerateID(crypto.INSTANCE_NODE)

// 	for {
// 		line, err := reader.ReadBytes('\n')
// 		if err != nil {
// 			return
// 		}

// 		var request protocol.Message

// 		err = json.Unmarshal(line, &request)
// 		if err != nil {
// 			fmt.Println("Invalid JSON:", err)
// 			continue
// 		}

// 		request.SessionID = sessionID

// 		respChan := make(chan protocol.Message)

// 		// sent request to worker
// 		api.controllerRequestChannel <- controllerRequest{
// 			data:     request,
// 			response: respChan,
// 		}

// 		// wait for response
// 		response := <-respChan

// 		respBytes, err := json.Marshal(response)
// 		if err != nil {
// 			fmt.Println("JSON encode error:", err)
// 			continue
// 		}

// 		respBytes = append(respBytes, '\n')

// 		_, err = writer.Write(respBytes)
// 		if err != nil {
// 			return
// 		}
// 		writer.Flush()
// 	}
// }

func (api *APIGateway) HandleHeartBeat(msg protocol.Message) {

}

func (api *APIGateway) NodeAsyncEvent(msg protocol.Message, conn *protocol.Connection) {
	switch msg.Type {
	case protocol.LB_SYNC:
		api.syncLoadBalancers(msg)
	default:
		log["console"].Error("Unsupported NodeAsyncEvent type: %s", msg.Type)
		log["console"].Debug("%v", msg)
	}
}

// -------------------- FUNCTIONS -----------------------
