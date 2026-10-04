package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestRequestIDMiddleware_GeneratesIDWhenMissing(t *testing.T) {
	var traceID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { traceID = middleware.GetTraceID(r.Context()) })
	rec := httptest.NewRecorder()

	middleware.RequestIDMiddleware(next).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	_, err := uuid.Parse(traceID)
	assert.NoError(t, err)
	assert.Equal(t, traceID, rec.Header().Get(middleware.RequestIDHeader))
}

func TestRequestIDMiddleware_KeepsIncomingID(t *testing.T) {
	var traceID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { traceID = middleware.GetTraceID(r.Context()) })
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(middleware.RequestIDHeader, "abc-123")
	rec := httptest.NewRecorder()

	middleware.RequestIDMiddleware(next).ServeHTTP(rec, req)

	assert.Equal(t, "abc-123", traceID)
	assert.Equal(t, "abc-123", rec.Header().Get(middleware.RequestIDHeader))
}

func TestGetTraceID_EmptyWithoutMiddleware(t *testing.T) {
	assert.Empty(t, middleware.GetTraceID(context.Background()))
}

func TestGetUserFromContext_ErrorsWithoutUser(t *testing.T) {
	_, err := middleware.GetUserFromContext(context.Background())
	assert.ErrorIs(t, err, middleware.ErrNoUserInContext)
}

func TestCorsMiddleware(t *testing.T) {
	h := middleware.CorsMiddleware([]string{"http://localhost:3000"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	preflight := func(origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/holdings", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		req.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	allowed := preflight("http://localhost:3000")
	assert.Equal(t, "http://localhost:3000", allowed.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, allowed.Header().Get("Access-Control-Allow-Methods"), http.MethodPost)

	assert.Empty(t, preflight("https://evil.example.com").Header().Get("Access-Control-Allow-Origin"))
}
