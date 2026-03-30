package main

import (
	"flag"
	"fmt"
	"networking/tcp/internal/agent"
)

func main() {
	//TODO: get port arg from exe
	var port int
	flag.IntVar(&port, "port", 2001, "Port that will be used by agent")
	flag.Parse()

	fmt.Println("Agent main: port -", port)

	agent, err := agent.NewAgent(fmt.Sprintf(":%d", port))
	if err != nil {
		fmt.Println("Error while creating agent", err)
	}

	agent.Start()

	select {}
}
