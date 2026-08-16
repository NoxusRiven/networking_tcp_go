package main

import (
	"flag"
	"fmt"
	"networking/tcp/internal/microservice"
	"strings"
)

func determineService(ms *microservice.Microservice, service_type string) {
	switch service_type {
	case "ping":
		ms.Service = &microservice.PingService{}
	case "idle":
		ms.Service = &microservice.IdleService{}
	}
}

func main() {
	var port int
	var service_type string

	flag.IntVar(&port, "port", 10001, "Port that will be used by service")
	flag.StringVar(&service_type, "type", "error", "Type of service intended to run")
	flag.Parse()

	ms, err := microservice.NewMicroservice(fmt.Sprintf(":%d", port))
	if err != nil {
		fmt.Println("Error while creating ms", err)
	}

	service_type = strings.ToLower(service_type)

	determineService(ms, service_type)

	ms.Start(service_type)
}
