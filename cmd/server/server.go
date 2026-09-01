package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	h "zest/pkg/api/handlers"
	"zest/pkg/infrastructure/config"
	"zest/pkg/infrastructure/scheduler"
)

// BasePath prefixes every route this service exposes.
const BasePath = "/api/v1"

// HTTPServer owns the gin engine, the configuration and the handler set.
type HTTPServer struct {
	Engine    *gin.Engine
	Config    *config.AppConfig
	Services  *h.Services
	Handlers  *h.Handlers
	Scheduler *scheduler.Scheduler
}

// engine pairs an *http.Server with a label used in boot/shutdown logs.
type engine struct {
	serverType string
	server     *http.Server
}

// NewServer builds the engine, installs middleware and wires the handlers.
func NewServer(cfg *config.AppConfig) (*HTTPServer, error) {
	addr := net.JoinHostPort(cfg.Server.Host, fmt.Sprint(cfg.Server.Port))
	log.Printf("%s: creating new server at %s", cfg.Server.Name, addr)

	gin.SetMode(GetEnvGinMode(cfg.Env))

	e := gin.New()
	e.Use(gin.Logger(), gin.Recovery())

	services := h.NewServices(cfg)

	sched, err := scheduler.New(services.Escalation, cfg.Report, cfg.Zendesk.Subdomain)
	if err != nil {
		return nil, fmt.Errorf("failed to build scheduler: %w", err)
	}

	return &HTTPServer{
		Engine:    e,
		Config:    cfg,
		Services:  services,
		Handlers:  h.New(cfg, services),
		Scheduler: sched,
	}, nil
}

// Run starts the HTTP server and blocks until it fails or a signal arrives.
func (s *HTTPServer) Run() {
	errs := make(chan error, 1)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	engines := []engine{
		{
			serverType: "Public",
			server: &http.Server{
				Addr:         net.JoinHostPort(s.Config.Server.Host, fmt.Sprint(s.Config.Server.Port)),
				Handler:      s.Engine,
				ReadTimeout:  s.Config.Server.ReadTimeout,
				WriteTimeout: s.Config.Server.WriteTimeout,
			},
		},
	}

	if err := s.Scheduler.Start(); err != nil {
		log.Printf("failed to start scheduler: %v", err)
	}

	for _, e := range engines {
		if e.server == nil {
			continue
		}

		go spawnServerRoutine(e.server, e.serverType, errs)
	}

	shutdown := s.gracefulShutdown(engines)

	select {
	case err := <-errs:
		shutdown(err)
	case sig := <-quit:
		shutdown(sig)
	}
}

func spawnServerRoutine(server *http.Server, serverType string, errChan chan<- error) {
	log.Printf("%s HTTP server started: %s", serverType, server.Addr)

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errChan <- err
	}
}

func (s *HTTPServer) gracefulShutdown(engines []engine) func(reason any) {
	return func(reason any) {
		log.Printf("shutting down %s: %+v", s.Config.Server.Name, reason)

		ctx, cancel := context.WithTimeout(context.Background(), s.Config.Server.ShutdownTimeout)
		defer cancel()

		s.Scheduler.Stop()

		for _, e := range engines {
			if err := e.server.Shutdown(ctx); err != nil {
				log.Printf("gracefully shutting %s server, err: %+v", e.serverType, err)
			}
		}
	}
}
