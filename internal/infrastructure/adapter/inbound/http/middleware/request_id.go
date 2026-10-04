package middleware

import (
	"context"
	"net/http"
	"regexp"

	"github.com/google/uuid"
)

type contextKey string

const (
	RequestIDHeader              = "X-Request-Id"
	TraceIDContextKey contextKey = "traceId"
)

// A client-supplied ID ends up in logs and response headers, so only short, plain ones are kept.
var safeRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get(RequestIDHeader)
		if !safeRequestID.MatchString(reqID) {
			reqID = uuid.New().String()
		}

		ctx := context.WithValue(r.Context(), TraceIDContextKey, reqID)
		w.Header().Set(RequestIDHeader, reqID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetTraceID(ctx context.Context) string {
	if val, ok := ctx.Value(TraceIDContextKey).(string); ok {
		return val
	}
	return ""
}
