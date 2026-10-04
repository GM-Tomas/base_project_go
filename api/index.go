package handler

import (
	"context"
	"log"
	"net/http"
	"sync"

	"github.com/GM-Tomas/base_project_go/internal/infrastructure/app"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/config"
)

var (
	mu      sync.Mutex
	handler http.Handler
)

// Handler is the Vercel serverless entrypoint for the Go runtime.
func Handler(w http.ResponseWriter, r *http.Request) {
	h, err := getHandler()
	if err != nil {
		// Details stay in the function logs; they can include connection strings.
		log.Printf("init failed: %v", err)
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	h.ServeHTTP(w, r)
}

// getHandler builds the app once per warm instance. Unlike sync.Once, a failed build (e.g. the
// database was briefly unreachable on cold start) is retried on the next request instead of
// leaving the instance broken until Vercel recycles it.
func getHandler() (http.Handler, error) {
	mu.Lock()
	defer mu.Unlock()
	if handler != nil {
		return handler, nil
	}
	application, err := app.BuildApp(context.Background(), config.LoadConfig())
	if err != nil {
		return nil, err
	}
	handler = application.Handler
	return handler, nil
}
