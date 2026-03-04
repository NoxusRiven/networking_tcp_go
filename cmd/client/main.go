package main

import (
	"flag"
	"log"
	"networking/tcp/internal/client"
	"os"
)

func main() {
	addr := flag.String("addr", "", "Adress to connect to")
	flag.Parse()

	if *addr == "" {
		log.Println("No Server address provided")
		flag.Usage()
		os.Exit(1)
	}

	if err := client.Start(*addr); err != nil {
		log.Fatal(err)
	}
}
