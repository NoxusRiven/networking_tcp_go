package main

import (
	"fmt"
	"networking/tcp/internal/protocol"
)

func main() {
	ms := &protocol.MsInfo{
		ID:   "123",
		Type: "PING",
		Host: "localhost",
		Port: "20000",
	}

	//fmt.Println(string(ms))
	fmt.Printf("%#v", ms)
}
