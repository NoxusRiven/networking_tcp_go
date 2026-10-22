package main

import (
	"fmt"
	"networking/tcp/internal/api"
	"networking/tcp/internal/config"
	"os"
)

func main() {
	conf, err := config.ReadConfigFile()
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	//use flags to get data to api like address
	apiNode, err := conf.FindNodeByName(config.API_CONF)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	api_gateway, err := api.NewAPIGateway(apiNode.Port)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	addr, err := conf.FindAddrByNodeName(config.CONTROLLER_CONF)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	err = api_gateway.Start(addr)
	if err != nil {
		fmt.Println("Error:", err)
	}
}
