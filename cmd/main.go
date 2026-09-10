package main

import (
	"fmt"
	"log"
	"os"

	"zest/cmd/server"
	"zest/pkg/infrastructure/config"
)

func main() {
	var (
		env            = os.Getenv("ENVIRONMENT")
		configFilePath = os.Getenv("CONFIG_FILE_PATH")
		secretFilePath = os.Getenv("SECRET_FILE_PATH")
	)

	log.Printf("ENVIRONMENT: %s, CONFIG_FILE_PATH: %s, SECRET_FILE_PATH: %s", env, configFilePath, secretFilePath)

	conf, err := config.LoadConfig(configFilePath, secretFilePath, env)
	if err != nil {
		panic(fmt.Sprintf("Failed to read config/secret file, err: %v. Shutting down.", err))
	}

	s, err := server.NewServer(conf)
	if err != nil {
		panic(fmt.Sprintf("Failed to start server, err: %v. Shutting down.", err))
	}

	s.SetupRoutes()

	s.Run()
}
