package api

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net"
	crypto "networking/tcp/internal/crypto"
	json "networking/tcp/internal/json"
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

	privKey *rsa.PrivateKey
}

// ##################################### STRUCTURES #####################################

// ################################## CORE #####################################

func NewAPIGateway(port string) (*APIGateway, error) {
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return nil, err
	}

	log["console"].Info("API Gateway listining on [::]: %v", port)

	return &APIGateway{
		listener: ln,
		lbInfo:   make(map[string]*protocol.LBalancerInfo),
		lbConn:   make(map[string][]*protocol.Connection),
	}, nil
}

func (api *APIGateway) genereateKeyFiles() {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	privateBytes, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		panic(err)
	}

	privatePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateBytes,
	})

	err = os.WriteFile("private.pem", privatePEM, 0600)
	if err != nil {
		panic(err)
	}

	publicBytes, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		panic(err)
	}

	publicPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicBytes,
	})

	err = os.WriteFile("public.pem", publicPEM, 0644)
	if err != nil {
		panic(err)
	}

	fmt.Println("RSA keys successfully generated!")

	if !api.findKeyFiles() {
		panic("Keys not found after generating them!")
	}
}

func (api *APIGateway) findKeyFiles() bool {
	privateData, err := os.ReadFile("private.pem")
	if err != nil {
		log["console"].Warning("Couldn't read private key file: %v", err)
		return false
	}

	privateBlock, _ := pem.Decode(privateData)
	if privateBlock == nil {
		log["console"].Error("Couldn't decode private key data: %v", err)
		return false
	}

	privateParsed, err := x509.ParsePKCS8PrivateKey(privateBlock.Bytes)
	if err != nil {
		log["console"].Error("Couldn't parse private key data: %v", err)
		return false
	}

	privKey, ok := privateParsed.(*rsa.PrivateKey)
	if !ok {
		log["console"].Error("Couldn't convert private key data to private key structure - Private key is not RSA")
		return false
	}

	api.privKey = privKey

	//public key will be derived from private, but public key file must exist
	_, err = os.ReadFile("public.pem")
	if err != nil {
		log["console"].Error("Couldn't read public key file: %v", err)
		return false
	}

	fmt.Println("Keys successfully loaded!")

	return true
}

func (api *APIGateway) Start(controllerAddr string) error {

	foundKeys := api.findKeyFiles()
	if !foundKeys {
		api.genereateKeyFiles()
	}

	api.controllerRequestChannel = make(chan *MessageExchange)

	api.requestChannelMap = make(map[string]chan *MessageExchange)

	log["console"].Debug("Trying to connect to controller %v", controllerAddr)
	//for now force connection to controller (only endpoint to system), if later many controllers will be present connect to 'backup' one?
	conn, err := protocol.TryToConnect(controllerAddr, 30*time.Second)
	if err != nil {
		return err
	}

	api.controllerConn = conn

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
		return logger.StrToError(log, "Controller responded with %v during handshake with api", respHello)
	}

	log["console"].Debug("Registered succesfully %v", respHello)

	// respSync := <-respChan

	// // first sync will happend after registration, every other will happen asynchronously
	// err = api.syncLoadBalancers(respSync)
	// if err != nil {
	// 	return err
	// }

	// log["console"].Debug("Sync response from controller %v", respSync)

	return nil
}

// checks if connection was present and updadtes data, if not creates new one
func (api *APIGateway) handleConnectionToLB(lb *protocol.LBalancerInfo) {

	//if connections exists, dont create new MsgExchnge and delete previous data
	conns, okConn := api.lbConn[lb.Base.ID]
	var conn *protocol.Connection
	if !okConn {
		log["console"].Debug("creating new MsgExchnge")
		api.requestChannelMap[lb.Base.ID] = make(chan *MessageExchange, CONNECTION_NUM)
	}

	// for every loadbalancer start CONNECTION_NUM of connections
	for i := 0; i < CONNECTION_NUM; i++ {

		//? Need 'raw' net.Dial, maybe can use TryConnect - look it up
		nc, err := net.DialTimeout("tcp", net.JoinHostPort(lb.Base.Host, lb.Base.Port), 3*time.Second)
		if err != nil {
			log["console"].Error("Error connecting to loadbalancer %s:%s, error: %v", lb.Base.Host, lb.Base.Port, err)
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
			log["console"].Warning("Error while sending request %v", err)
			continue
		}
		resp, ok := <-respChan

		if !ok {
			log["console"].Warning("Channel closed")
			continue
		}

		if resp.Code != protocol.SUCCESS {
			log["console"].Warning("Loadbalancer %s:%s responsed with error code %d to heartbeat on %s connID, response: %v", lb.Base.Host, lb.Base.Port, resp.Code, conn.ID, resp)
			continue
		}

		//successfully connected to lb, now map them to thier data structures
		log["console"].Debug("Response from loadbalancer %s:%s on conn(ID-%s, LocAddr-%s): %v", lb.Base.Host, lb.Base.Port, conn.ID, conn.Nc.LocalAddr(), resp)

		if !okConn {
			api.lbInfo[conn.ID] = lb
			api.lbConn[lb.Base.ID] = append(api.lbConn[lb.Base.ID], conn)
		}

		go api.messageWorker(conn)
	}
}

func (api *APIGateway) syncLoadBalancers(msg protocol.Message) error {

	lbList, err := json.FromJSON[[]*protocol.LBalancerInfo](msg.Content)
	if err != nil {
		return logger.StrToError(log, "Requested message: %v, error generated: %v", msg, err)
	}

	log["console"].Debug("Converted json into lb list")

	existingByID := make(map[string]*protocol.LBalancerInfo)
	for _, l := range api.lbInfo {
		existingByID[l.Base.ID] = l
	}

	hasLB := len(existingByID) > 0
	skip := make([]bool, len(lbList))

	for i, lb := range lbList {
		log["console"].Debug("LB: %s:%s, ID: %s", lb.Base.Host, lb.Base.Port, lb.Base.ID)

		if hasLB {
			if existingLb, exists := existingByID[lb.Base.ID]; exists {
				//change fields
				existingLb.Base.ID = lb.Base.ID
				existingLb.Base.NodeID = lb.Base.NodeID
				existingLb.Base.Host = lb.Base.Host
				existingLb.Base.Port = lb.Base.Port
				existingLb.Microservices = lb.Microservices
				existingLb.Base.Status = lb.Base.Status

				skip[i] = true
				log["console"].Debug("Updated existing LB data for ID: %s", lb.Base.ID)
			}
		}
	}

	for i, lb := range lbList {
		if !skip[i] {
			log["console"].Debug("Connecting to new LB: %s:%s, ID: %s", lb.Base.Host, lb.Base.Port, lb.Base.ID)
			api.handleConnectionToLB(lb)
		}
	}
	return nil
}

func (api *APIGateway) messageWorker(conn *protocol.Connection) {
	lb := api.lbInfo[conn.ID]

	for msgEx := range api.requestChannelMap[lb.Base.ID] {
		_, ok := api.lbInfo[conn.ID]
		if !ok {
			log["console"].Debug("None existant connection conn =  - stopping messageWroker\n")
			return
		}

		req := msgEx.request
		log["console"].Debug("Message worker got request: %v", req)

		go func(msgEx *MessageExchange) {

			err := conn.SendAndHandle(req, func(resp protocol.Message) error {
				log["console"].Debug("Message worker forwarding response: %v", resp)
				msgEx.response <- resp

				return nil
			})
			if err != nil {
				log["console"].Error("Error receiving message channel from loadbalancer on connID %s, error: %v", conn.ID, err)

				conn.Close()
				resp := protocol.Message{
					ID:      req.ID,
					Type:    req.Type,
					Code:    protocol.ERROR,
					Content: "Unable to connect to server",
				}
				msgEx.response <- resp

				return
			}
		}(msgEx)
	}

	//protocol.TryToConnect()
}

func (api *APIGateway) handleCliHandshake(conn *protocol.Connection) (protocol.Message, []byte) {
	var resp protocol.Message

	var clientPubKey *rsa.PublicKey
	var aesKey []byte
	var encKey string

	msg, err := protocol.Receive(conn.RW.Reader)
	if err != nil {
		log["console"].Error("Error while reading handshake from client: %v", err)
		resp = protocol.Message{
			ID:   msg.ID,
			Type: protocol.HELLO,
			Code: protocol.ERROR,
		}
		goto send
	}

	clientPubKey, err = crypto.ParsePublicKeyBase64(msg.Keys.CliPubKey)
	if err != nil {
		log["console"].Error("api: invalid client public key: %v", err)
		resp = protocol.Message{
			ID:   msg.ID,
			Type: protocol.HELLO,
			Code: protocol.ERROR,
		}
		goto send
	}

	aesKey, err = crypto.GenerateAESKey()
	if err != nil {
		log["console"].Error("api: couldn't generate aes key: %v", err)
		resp = protocol.Message{
			ID:   msg.ID,
			Type: protocol.HELLO,
			Code: protocol.ERROR,
		}
		goto send
	}

	encKey, err = crypto.EncryptSessionKey(clientPubKey, aesKey)
	if err != nil {
		log["console"].Error("api: encrypt handshake ack: %v", err)
		resp = protocol.Message{
			ID:   msg.ID,
			Type: protocol.HELLO,
			Code: protocol.ERROR,
		}
		goto send
	}

	resp = protocol.Message{
		ID:   msg.ID,
		Type: protocol.HELLO,
		Code: protocol.SUCCESS,
		Keys: crypto.KeyPayload{
			AesKey: encKey,
		},
	}

send:
	if err := protocol.Send(conn.RW.Writer, resp); err != nil {
		log["console"].Error("api: failed to send handshake response: %v", err)
	}

	return resp, aesKey
}

func (api *APIGateway) handleClient(cliConn *protocol.Connection) {
	defer cliConn.Close()

	resp, cliAes := api.handleCliHandshake(cliConn)
	if resp.Code != protocol.SUCCESS {
		return
	}

	log["console"].Info("Succesfull handshake with client - messages will be encrypted, aes: %v", cliAes)

	for {
		cliRequest, err := protocol.Receive(cliConn.RW.Reader)
		if err != nil {
			log["console"].Error("Error while reading request from client: %v", err)
			return
		}

		log["console"].Debug("Received request from client: %v", cliRequest)

		if cliRequest.Type == "EXIT" {
			log["console"].Info("Client disconnected")
			return
		}

		reqDecrypted, err := crypto.DecryptAES(cliRequest.Content, cliAes)
		if err != nil {
			log["console"].Error("Error while trying to decrypt request request from client: %v", err)
			return
		}

		log["console"].Debug("Decrypted cli message %v", cliRequest.Content)

		request := cliRequest
		request.Content = reqDecrypted
		request.Keys = crypto.KeyPayload{}

		reqPool := &MessageExchange{
			request:  request,
			response: make(chan protocol.Message, 8),
		}

		lb := api.findLbForRequest(request)
		if lb == nil {
			return
		}

		api.requestChannelMap[lb.Base.ID] <- reqPool

		var responseCli protocol.Message
		for response := range reqPool.response {
			response.ID = request.ID

			//log["console"].Debug("trying to send response to client %v", response)
			encDataBytes, err := crypto.EncryptAES(response.Content, cliAes)

			encData := base64.StdEncoding.EncodeToString(encDataBytes)

			responseCli = response
			responseCli.Content = encData

			cliConn.Mu.Lock()
			err = protocol.Send(cliConn.RW.Writer, responseCli)
			cliConn.Mu.Unlock()

			if err != nil {
				log["console"].Error("Error while sending response to client: %v", err)
				return
			}

			log["console"].Debug("Sent response to client: %v", responseCli)

			if !response.IsStream {
				close(reqPool.response)
			}
		}

		// //! temporary delete later
		// code := protocol.SUCCESS

		// if err != nil {
		// 	data = err.Error()
		// 	code = protocol.ERROR
		// }

		// response := protocol.Message{
		// 	ID:      cliRequest.ID,
		// 	Type:    cliRequest.Type,
		// 	Code:    code,
		// 	Content: data,
		// }
		// err = protocol.Send(cliConn.RW.Writer, response)
		// if err != nil {
		// 	log["console"].Error("Error while sending response to client: %v", err)
		// 	return
		// }
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

		if resp.Content == "null" {
			log["console"].Error("Empty lb list: %v", resp)
		}

		log["console"].Debug("Response from controller after asking to create new lb for request: %v", resp)

		if err != nil {
			log["console"].Error("Error while sending request to controller to create %v ms for request: %v", request.Content, err)
			return nil
		}

		//api.update
		if resp.Type != protocol.UPDATE {
			log["console"].Error("Controller responded with type %d to  request for creating %v ms, response: %v", resp.Type, request.Content, resp)
			return nil
		}

		lbUpdate, err := json.FromJSON[*protocol.LBalancerInfo](resp.Content)
		if err != nil {
			log["console"].Error("Unable to covert from json to lb: %v", err)
			return nil
		}

		//if no lb conns are present connect to new found lb
		if len(api.lbConn) == 0 || len(api.lbInfo) == 0 {
			api.handleConnectionToLB(lbUpdate)
		}

		//get first connection to lb
		lbconn := api.lbConn[lbUpdate.Base.ID][0]
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

	//go conn.ReceiveLoopChan(api)

	resp, err := conn.SendRequest(protocol.Message{
		Type:    protocol.HELLO,
		Content: api.String(),
	})
	if err != nil {
		log["console"].Error("Error while sending request: %v", err)
		conn.Close()

		return
	}

	switch resp.Content {
	case "LOADBALANCER":
		lb := api.lbInfo[conn.ID]

		if resp.Code != protocol.SUCCESS {
			log["console"].Error("Loadbalancer %s:%s responsed with error code %d to heartbeat on %s connID, response: %v", lb.Base.Host, lb.Base.Port, resp.Code, conn.ID, resp)
			return
		}

		go api.messageWorker(conn)
	case "CONTROLLER":
		if resp.Code != protocol.SUCCESS {
			log["console"].Error("Controller %s:%s responsed with ", resp)
			return
		}

		log["console"].Debug("Controller responded with: %v", resp)

	default:
		log["console"].Error("Unknow node responded with HELLO: %v", resp)

	}

}

func (api *APIGateway) ReceiveHeartBeat(msg protocol.Message, conn *protocol.Connection) {

}

func (api *APIGateway) AsyncEvent(msg protocol.Message, conn *protocol.Connection) {
	switch msg.Type {
	case protocol.UPDATE:
		code := protocol.SUCCESS
		if err := api.syncLoadBalancers(msg); err != nil {
			log["console"].Error("Async event - syncLoadBalancer(): %v", err)
			code = protocol.ERROR
		}

		if err := protocol.Send(conn.RW.Writer, protocol.Message{
			ID:   msg.ID,
			Type: msg.Type,
			Code: code,
		}); err != nil {
			log["console"].Error("Async event - Send(): %v", err)
			return
		}
	default:
		//log["console"].Debug("Forwarding to Client %v", msg)

		log["console"].Error("Unsuported async msg: %v", msg)
		return
	}
}

func (api *APIGateway) String() string {
	return "API"
}

// ################################# NODE METHODS #################################
