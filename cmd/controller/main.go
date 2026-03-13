package main

import "networking/tcp/internal/controller"

func main() {

	controller, err := controller.NewController()
	if err != nil {
		panic(err)
	}

	controller.Start()
}
