package main

import "fmt"

type MsInfo struct {
	agent     string
	lBalancer string
	host      string
	port      string
}

func main() {
	_map := make(map[string]MsInfo)

	//_map["login"] = MsInfo{agent: "agent", lBalancer: "balancer", host: "host", port: "port"}

	info, found := _map["login"]

	fmt.Println(found, info)

}
