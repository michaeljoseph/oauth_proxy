package main

import (
	"log"
	"os"
	"os/exec"
)

func execute(command string, authToken string) bool {
	env := os.Environ()
	env = append(env, "AUTH_TOKEN="+authToken)
	cmd := exec.Command(command)
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		log.Printf("Unable to run external authentication command: %s", err)
		return false
	}
	return true
}

func NewCommandValidator(command string) func(string) bool {

	validator := func(authToken string) bool {
		return execute(command, authToken)
	}
	return validator
}
