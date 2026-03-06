package main

import "networking/tcp/internal/controller"

func main() {

	controller, err := controller.NewController(8888)
	if err != nil {
		panic(err)
	}

	err = controller.Start()
	if err != nil {
		panic(err)
	}
}
