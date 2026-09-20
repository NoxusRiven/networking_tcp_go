package client

import (
	"bufio"
	"fmt"
	"net"
	"networking/tcp/internal/protocol"
	"os"
	"strings"
)

// -------------------- STRUCTURES -----------------------
type CLI struct {
	conn      *protocol.Connection
	isRunning bool
}

// -------------------- FUNCTIONS -----------------------
func NewCLI() *CLI {
	return &CLI{
		isRunning: false,
	}
}

// Run connects to API Gateway
func (c *CLI) Run(host string, port string) error {
	nc, err := net.Dial("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}

	c.conn = protocol.NewConnection(nc)
	defer c.conn.Close()

	go c.conn.ReceiveLoopNew(c)

	c.isRunning = true

	for c.isRunning {
		fmt.Println(
			"Hello! What would you like to do?\n" +
				"(Select number below):\n" +
				"1. Ping Server\n" +
				"2. Do Idle Work\n" +
				"3. Post Message (not implemented)\n" +
				"0. Exit program",
		)

		input := readUserInput("> ")

		switch input {
		case "1":
			c.HandlePing()
		case "2":
			//TODO: maybe use go to use cli while streaming happens
			c.HandleIdle()
		case "3":
			fmt.Println("Not implemented. Yet...")
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

// ################################ MESSAGE HANDLERS ################################
func (c *CLI) HandlePing() {
	msg := protocol.Message{
		Type: "PING",
	}

	respChan, err := c.conn.SendRequestNew(msg)
	if err != nil {
		fmt.Println("[ERROR]: Error while seding message to API", err)

		return
	}

	resp := <-respChan

	fmt.Println("[DEBUG]: Server Full Response:", resp)

	fmt.Println("[SERVER]:", resp.Content)
}

func (c *CLI) HandleIdle() {
	msg := protocol.Message{
		Type: "IDLE",
	}

	respChan, err := c.conn.SendRequestNew(msg)
	if err != nil {
		fmt.Println("[ERROR]: Problem accured when SendRequestNew returned channel:", err)

	}

	for resp := range respChan {
		//resp := c.conn.ReceiveRequestNew(msg)
		fmt.Println("[HandleIdle]: response from API: ", resp)
	}

	fmt.Println("[INFO]: Idle has ended!")
}

func (c *CLI) HandleExit() {
	c.isRunning = false
	msg := protocol.Message{
		Type: "EXIT",
	}

	//ignoring error because client is exiting
	_ = protocol.Send(c.conn.RW.Writer, msg)
}

// ################################ MESSAGE HANDLERS ################################

// ################################ NODE METHODS ################################

func (c *CLI) HandleHeartBeat(msg protocol.Message) {
	// client doesnt get heartbeat checks
}

func (c *CLI) NodeAsyncEvent(msg protocol.Message, conn *protocol.Connection) {
	// client always should be pending for messages so if any message is directed here it is a bug

	fmt.Println("[ERROR]: Incorrect behaviour! Client received message '", msg, "' in NodeAsyncEvent() even though CLI always expects response.")
}

// ################################ NODE METHODS ################################
