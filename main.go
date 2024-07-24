package main

import (
	"log"

	"github.com/helen-frank/mix-scheduler-admission-webhook/pkg/webhook/server"
)

func main() {
	err := server.StartServer()
	if err != nil {
		log.Fatal(err)
	}
}
