package main

// import (
// 	"flag"
// 	"log"
// 	"networking/tcp/internal/api"
// 	"os"
// )

// func main() {
// 	addr := flag.String("addr", "", "API Gateway address to listen on")
// 	flag.Parse()

// 	if *addr == "" {
// 		log.Println("No Api Gateway address provided")
// 		flag.Usage()
// 		os.Exit(1)
// 	}

// 	if err := api.Start(*addr); err != nil {
// 		log.Fatal(err)
// 	}
// }

import (
	"fmt"
	"networking/tcp/internal/api"
	"os"
)

func main() {
	//use flags to get data to api like address
	api_gateway, err := api.NewAPIGateway(6969)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	err = api_gateway.Start("localhost:8888")
	if err != nil {
		fmt.Println("Error:", err)
	}
}
