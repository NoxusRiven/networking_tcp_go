package client

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"networking/tcp/internal/config"
	"networking/tcp/internal/crypto"
	"networking/tcp/internal/protocol"
	"os"
	"strings"
	"time"
)

// ####################### STRUCTURES ##########################
type Session struct {
	apiConn    *protocol.Connection
	privKey    *rsa.PrivateKey
	sessionAes []byte
}

type CLI struct {
	isRunning bool
	apiPubKey *rsa.PublicKey

	session Session
}

// ####################### STRUCTURES ##########################

// ################################ FUNCTIONS ################################
func NewCLI() *CLI {
	return &CLI{
		isRunning: false,
	}
}

func (c *CLI) readApiKey() error {
	pubData, err := os.ReadFile("api_pub.pem")
	if err != nil {
		return fmt.Errorf("Couldn't read api public key file: %v", err)
	}

	pubBlock, _ := pem.Decode(pubData)
	if pubBlock == nil {
		return fmt.Errorf("Couldn't decode api public key data: %v", err)
	}

	pubParsed, err := x509.ParsePKIXPublicKey(pubBlock.Bytes)
	if err != nil {
		return fmt.Errorf("Couldn't parse api public key data: %v", err)
	}

	apiPubKey, ok := pubParsed.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("Couldn't convert api public key data to private key structure - Private key is not RSA")
	}

	c.apiPubKey = apiPubKey

	fmt.Printf("Read key from file - API pubKey: %v\n", c.apiPubKey.Size())

	return nil
}

// Run setups CLI and connects to API Gateway
func (c *CLI) Run() error {

	conf, err := config.ReadConfigFile()
	if err != nil {
		return err
	}

	addr, err := conf.FindAddrByNodeName(config.API_CONF)
	if err != nil {
		return err
	}

	//read api key
	err = c.readApiKey()
	if err != nil {
		return err
	}

	c.session.apiConn, err = protocol.TryToConnect(addr, 5*time.Second)
	if err != nil {
		return err
	}

	defer c.session.apiConn.Close()

	go c.session.apiConn.ReceiveLoopChan(c)

	err = c.PerformHandshake()
	if err != nil {
		return err
	}

	c.isRunning = true

	for c.isRunning {
		fmt.Println(
			"Hello! What would you like to do?\n" +
				"(Select number below):\n" +
				"1. Ping Server\n" +
				"2. Do Idle Work\n" +
				"0. Exit program",
		)

		input := readUserInput("> ")

		switch input {
		case "1":
			c.HandlePing()
		case "2":
			//TODO: maybe use go to use cli while streaming happens
			c.HandleIdle()
		case "0":
			c.HandleExit()
		default:
			fmt.Println("Invalid input, please try again.")
		}
		fmt.Println()
	}

	return nil
}

// readUserInput reads a trimmed line from stdin
func readUserInput(prompt string) string {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}

func (c *CLI) PerformHandshake() error {
	privKey, err := rsa.GenerateKey(rand.Reader, crypto.RsaKeyBits)
	if err != nil {
		return fmt.Errorf("cli: generate RSA keypair: %w", err)
	}

	c.session.privKey = privKey

	pubKeyB64, err := crypto.MarshalPublicKeyBase64(&c.session.privKey.PublicKey)
	if err != nil {
		return fmt.Errorf("cli: marshal public key: %w", err)
	}

	handshakeMsg := protocol.Message{
		Type: protocol.HELLO,
		Keys: crypto.KeyPayload{
			CliPubKey: pubKeyB64,
		},
	}

	err = c.SendAndHandleCypher(handshakeMsg, func(resp protocol.Message) error {
		if resp.Type != protocol.HELLO {
			return fmt.Errorf("cli: unexpected handshake response type: %s", resp.Type)
		}
		if len(c.session.sessionAes) == 0 {
			return fmt.Errorf("cli: missing session AES key after handshake")
		}

		fmt.Printf("Received handshake: %v", resp)
		return nil
	})
	if err != nil {
		return fmt.Errorf("cli: handshake failed: %w", err)
	}

	fmt.Println("Succesfull handshake performed with API - aes:", c.session.sessionAes)

	return nil
}

// SendAndHandleCypher attaches the client's public key to msg, sends it to the API,
// resolves and caches the AES session key when provided, decrypts the response payload,
// and forwards the decrypted message to handler.
func (c *CLI) SendAndHandleCypher(
	msg protocol.Message,
	handler func(protocol.Message) error,
) error {
	if c.session.privKey == nil {
		return fmt.Errorf("protocol: client private key must not be nil")
	}
	if handler == nil {
		return fmt.Errorf("protocol: handler must not be nil")
	}

	pubKeyDER, err := x509.MarshalPKIXPublicKey(&c.session.privKey.PublicKey)
	if err != nil {
		return fmt.Errorf("protocol: marshal client public key: %w", err)
	}

	req := msg
	req.Keys.CliPubKey = base64.StdEncoding.EncodeToString(pubKeyDER)

	contentBytes, err := crypto.EncryptAES(msg.Content, c.session.sessionAes)
	req.Content = base64.StdEncoding.EncodeToString(contentBytes)

	wrappedHandler := func(resp protocol.Message) error {
		var aesKey []byte

		if resp.Code != protocol.SUCCESS {
			return fmt.Errorf("Wrong response code %v", resp)
		}

		// 1. Jeśli odpowiedź niesie nowy klucz AES (np. podczas handshake),
		// odszyfruj go kluczem prywatnym RSA i zapisz w sesji.
		if resp.Keys.AesKey != "" {
			decryptedKey, err := crypto.DecryptSessionKey(c.session.privKey, resp.Keys.AesKey)
			if err != nil {
				return fmt.Errorf("protocol: decrypt session key: %w", err)
			}
			c.session.sessionAes = decryptedKey
			aesKey = decryptedKey
		} else {
			// 2. W kolejnych wiadomościach używamy zcacheowanego klucza sesyjnego AES.
			aesKey = c.session.sessionAes
		}

		decryptedResp := resp

		// Jeśli treść odpowiedzi nie jest pusta, odszyfruj ją kluczem AES.
		if resp.Content != "" {
			if len(aesKey) == 0 {
				return fmt.Errorf("protocol: no AES session key available to decrypt response")
			}

			decryptedContent, err := crypto.DecryptAES(resp.Content, aesKey)
			if err != nil {
				return fmt.Errorf("protocol: decrypt response: %w", err)
			}
			decryptedResp.Content = decryptedContent
		}

		return handler(decryptedResp)
	}

	if err := c.session.apiConn.SendAndHandle(req, wrappedHandler); err != nil {
		return fmt.Errorf("protocol: send and handle encrypted response: %w", err)
	}

	return nil
}

// ################################ FUNCTIONS ################################

// ################################ MESSAGE HANDLERS ################################
func (c *CLI) HandlePing() {
	msg := protocol.Message{
		Type: "PING",
	}

	if err := c.SendAndHandleCypher(msg, func(resp protocol.Message) error {
		fmt.Println("[DEBUG]: Server Full Response:", resp)

		fmt.Println("[SERVER]:", resp.Content)

		return nil
	}); err != nil {
		fmt.Println("Error SendAndHandle:", err)
		return
	}

}

func (c *CLI) HandleIdle() {
	msg := protocol.Message{
		Type: "IDLE",
	}

	if err := c.SendAndHandleCypher(msg, func(resp protocol.Message) error {
		fmt.Println("[DEBUG]: Server Full Response:", resp)

		if resp.Content != "" {
			fmt.Println("[SERVER]:", resp.Content)
		} else {
			fmt.Println("[SERVER]: Idle has ended!")
		}

		return nil
	}); err != nil {
		fmt.Println("Error SendAndHandle:", err)
		return
	}

}

func (c *CLI) HandleExit() {
	c.isRunning = false
	msg := protocol.Message{
		Type: "EXIT",
	}

	//ignoring error because client is exiting
	_ = protocol.Send(c.session.apiConn.RW.Writer, msg)
}

// ################################ MESSAGE HANDLERS ################################

// ################################ NODE METHODS ################################

func (c *CLI) HandShake(conn *protocol.Connection) {

}

func (c *CLI) ReceiveHeartBeat(msg protocol.Message, conn *protocol.Connection) {
	// client doesnt receive heartbeat checks
}

func (c *CLI) AsyncEvent(msg protocol.Message, conn *protocol.Connection) {
	// client always should be pending for messages so if any message is directed here it is a bug

	fmt.Println("[ERROR]: Incorrect behaviour! Client received message '", msg, "' in NodeAsyncEvent() even though CLI always expects response.")
}

func (c *CLI) String() string {
	return "Client"
}

// ################################ NODE METHODS ################################
