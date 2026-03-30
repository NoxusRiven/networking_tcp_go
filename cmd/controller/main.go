package main

import (
	"fmt"
	"networking/tcp/internal/controller"
	"os"
	"os/signal"
	"syscall"
)

func main() {

	controller, err := controller.NewController()
	if err != nil {
		panic(err)
	}

	controller.Start()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	sig := <-sigChan
	fmt.Println("Received signal:", sig)

	controller.KillAllAgents()

	fmt.Println("Controller shutdown complete")
	os.Exit(1)
	select {}
}
