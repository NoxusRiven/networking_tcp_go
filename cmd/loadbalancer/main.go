package main

import (
	"flag"
	"fmt"
	"networking/tcp/internal/loadbalancer"
)

func main() {
	var port int
	flag.IntVar(&port, "port", 1111, "Port that will be used by agent")
	flag.Parse()

	lb, err := loadbalancer.NewLoadBalancer(fmt.Sprintf(":%d", port))
	if err != nil {
		fmt.Println("Error while creating loadbalancer", err)
	}

	lb.Start()

	select {}
}
