package main

import (
	"fmt"
	"os/exec"
)

func main() {
	path, err := exec.LookPath("../test_file.txt")
	if err != nil {
		fmt.Println("File not found!")
		return
	}

	fmt.Println("File found in path:", path)

	cmd := exec.Command("../agent.exe")
	if err = cmd.Start(); err != nil {
		fmt.Println("Error while executing command: ", err)
	}
}
