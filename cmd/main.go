package main

import (
	"fmt"
	"log"
	"os"

	"zest/cmd/server"
	"zest/pkg/infrastructure/config"
)

func main() {
	log.Printf("ENVIRONMENT: %s", os.Getenv("ENVIRONMENT"))

	conf, err := config.LoadConfig()
	if err != nil {
		panic(fmt.Sprintf("Failed to load config, err: %v. Shutting down.", err))
	}

	s, err := server.NewServer(conf)
	if err != nil {
		panic(fmt.Sprintf("Failed to start server, err: %v. Shutting down.", err))
	}

	s.SetupRoutes()

	s.Run()
}
