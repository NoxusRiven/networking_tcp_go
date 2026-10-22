package main

import (
	"fmt"
	"networking/tcp/internal/client"
)

func main() {
	cli := client.NewCLI()
	if err := cli.Run(); err != nil {
		fmt.Println("CLI error:", err)
	}
}
