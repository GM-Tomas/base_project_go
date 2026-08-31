package middleware

import (
	"net/http"

	"github.com/go-chi/cors"
)

func CorsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	return cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Request-Id"},
		ExposedHeaders:   []string{"Link", "Location", "X-Request-Id"},
		AllowCredentials: false,
		MaxAge:           3600,
	})
}
