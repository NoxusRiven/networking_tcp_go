package client

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

func Start(address string) error {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return err
	}
	defer conn.Close()

	fmt.Println("Connected to server")

	done := make(chan bool)

	go handle(conn, done)

	//wait for quit signal
	<-done

	fmt.Println("Client exiting")
	return nil
}

func handle(conn net.Conn, done chan bool) {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")

		text, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("input error:", err)
			done <- true
			return
		}

		text = strings.ToLower(text)
		text = strings.TrimSpace(text)

		if text == "exit" || text == "quit" {
			fmt.Println("Exiting...")
			done <- true
			return
		}

		// send to server
		_, err = conn.Write([]byte(text))
		if err != nil {
			fmt.Println("write error:", err)
			done <- true
			return
		}

		// read response
		buf := make([]byte, len(text))
		_, err = conn.Read(buf)
		if err != nil {
			fmt.Println("read error:", err)
			done <- true
			return
		}

		fmt.Println("Server:", string(buf))
	}
}
