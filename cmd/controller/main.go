package main

import (
	"fmt"
	"networking/tcp/internal/controller"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	var sigChan chan os.Signal
	var sig os.Signal

	controller := controller.NewController()
	err := controller.Start()
	if err != nil {
		fmt.Println("Controller error:", err)
		goto cleanup
	}

	sigChan = make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	sig = <-sigChan
	fmt.Println("Received signal:", sig)

cleanup:
	controller.KillAllAgents()
	controller.KillAllLoadBalancers()

	fmt.Println("Controller shutdown complete")
	os.Exit(1)
	select {}
}
