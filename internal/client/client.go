package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"networking/tcp/internal/protocol"
	"os"
	"strings"
)

// -------------------- STRUCTURES -----------------------
type CLI struct {
	conn      net.Conn
	isRunning bool
	reader    *bufio.Reader
	writer    *bufio.Writer
}

// -------------------- FUNCTIONS -----------------------
func NewCLI() *CLI {
	return &CLI{
		isRunning: false,
	}
}

// Run connects to API Gateway
func (c *CLI) Run(host string, port string) error {
	conn, err := net.Dial("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	c.conn = conn
	c.reader = bufio.NewReader(conn)
	c.writer = bufio.NewWriter(conn)
	c.isRunning = true

	for c.isRunning {
		fmt.Println(
			"Hello! What would you like to do?\n" +
				"(Select number below):\n" +
				"1. Ping Server\n" +
				"2. Post Message (not implemented)\n" +
				"0. Exit program",
		)

		input := readUserInput("> ")

		switch input {
		case "1":
			c.HandlePing()
		case "2":
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

// -------------------- MESSAGE HANDLERS -----------------------
func (c *CLI) HandlePing() {
	msg := protocol.Message{
		Type: "PING",
	}

	if err := c.SendMessage(msg); err != nil {
		fmt.Println("Send error:", err)
		return
	}

	resp, err := c.ReceiveMessage()
	if err != nil {
		fmt.Println("Receive error:", err)
		return
	}

	fmt.Println("[DEBUG]: Server Full Response:", resp)

	fmt.Println("[SERVER]:", resp.Content)
}

func (c *CLI) HandleExit() {
	c.isRunning = false
	msg := protocol.Message{
		Type: "EXIT",
	}
	_ = c.SendMessage(msg)
	c.conn.Close()
}

// -------------------- JSON SEND/RECEIVE -----------------------
func (c *CLI) SendMessage(msg protocol.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n') // delimiter
	_, err = c.writer.Write(data)
	if err != nil {
		return err
	}
	return c.writer.Flush()
}

func (c *CLI) ReceiveMessage() (protocol.Message, error) {
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return protocol.Message{}, err
	}

	var msg protocol.Message
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return protocol.Message{}, err
	}

	return msg, nil
}
