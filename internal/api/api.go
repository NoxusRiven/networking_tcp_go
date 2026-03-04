package api

import (
	"fmt"
	"io"
	"net"
)

func Start(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	fmt.Println("Listening on", addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Println("accept error:", err)
			continue
		}
		go handleRequests(conn)
	}
}

func handleRequests(conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 1024)

	for {
		n, err := conn.Read(buf)
		if err != nil {
			if err != io.EOF {
				fmt.Println("read error:", err)
			}
			return
		}

		_, err = conn.Write(buf[:n])
		if err != nil {
			fmt.Println("write error:", err)
			return
		}
	}
}
