package main

import (
	"flag"
	"log"
	"networking/tcp/internal/api"
	"os"
)

func main() {
	addr := flag.String("addr", "", "API Gateway address to listen on")
	flag.Parse()

	if *addr == "" {
		log.Println("No Api Gateway address provided")
		flag.Usage()
		os.Exit(1)
	}

	if err := api.Start(*addr); err != nil {
		log.Fatal(err)
	}
}
