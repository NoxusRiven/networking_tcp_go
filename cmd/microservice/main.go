package main

import (
	"flag"
	"fmt"
	"networking/tcp/internal/microservice"
)

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

	ms.Start(service_type)
}
