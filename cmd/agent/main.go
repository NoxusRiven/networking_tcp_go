package main

import (
	"flag"
	"fmt"
	"networking/tcp/internal/agent"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	//TODO: get port arg from exe
	var port int
	flag.IntVar(&port, "port", 2001, "Port that will be used by agent")
	flag.Parse()

	agent, err := agent.NewAgent(fmt.Sprintf(":%d", port))
	if err != nil {
		fmt.Println("Error while creating agent", err)
	}

	agent.Start()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	sig := <-sigChan
	fmt.Println("Received signal:", sig)

	agent.KillAllMS()

	fmt.Println("Agent shutdown complete")
	os.Exit(1)

	select {}
}
