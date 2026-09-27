package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/infrastructure/app"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/config"
)

func main() {
	cfg := config.LoadConfig()

	log.Printf("Starting BASE Wealth API (Go) on port %s...", cfg.Port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	application, err := app.BuildApp(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}
	defer application.Cleanup()

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      application.Handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Server runner
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("BASE Wealth API is listening on http://localhost:%s", cfg.Port)
		log.Printf("Interactive Swagger UI available at http://localhost:%s/swagger", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	// Graceful Shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		log.Fatalf("Fatal server error: %v", err)
	case sig := <-shutdown:
		log.Printf("Received signal %v, shutting down server...", sig)

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Graceful shutdown failed: %v, forcing close", err)
			_ = server.Close()
		}
		log.Println("Server shutdown completed.")
	}
}
