package handler

import (
	"context"
	"net/http"
	"sync"

	"github.com/GM-Tomas/base_project_go/internal/infrastructure/app"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/config"
)

var (
	handler http.Handler
	initErr error
	once    sync.Once
)

// Handler is the official Vercel serverless entrypoint for Go runtime.
func Handler(w http.ResponseWriter, r *http.Request) {
	once.Do(func() {
		cfg := config.LoadConfig()
		application, err := app.BuildApp(context.Background(), cfg)
		if err != nil {
			initErr = err
			return
		}
		handler = application.Handler
	})

	if initErr != nil {
		http.Error(w, "Failed to initialize serverless application: "+initErr.Error(), http.StatusInternalServerError)
		return
	}

	if handler != nil {
		handler.ServeHTTP(w, r)
	} else {
		http.Error(w, "Serverless handler not initialized", http.StatusInternalServerError)
	}
}
