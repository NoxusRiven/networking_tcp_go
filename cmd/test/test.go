package main

import "fmt"

func main() {
	var mapTest map[string]string

	testValue, ok := mapTest["test"]

	if !ok {
		fmt.Println("Key not found")
	} else {
		fmt.Println("Value:", testValue)
	}
}
