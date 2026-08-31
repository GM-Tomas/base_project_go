package middleware_test

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthMiddleware_ValidToken(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	key, err := jwk.FromRaw(privKey.Public())
	require.NoError(t, err)
	_ = key.Set(jwk.KeyIDKey, "test-key")
	_ = key.Set(jwk.AlgorithmKey, jwa.RS256)

	keySet := jwk.NewSet()
	_ = keySet.AddKey(key)

	issuer := "https://supabase.example.com/auth/v1"
	audience := "authenticated"
	validator := middleware.NewStaticJWTValidator(keySet, issuer, audience)

	userId := uuid.New()
	tok, err := jwt.NewBuilder().
		Issuer(issuer).
		Audience([]string{audience}).
		Subject(userId.String()).
		IssuedAt(time.Now().Add(-1 * time.Minute)).
		Expiration(time.Now().Add(1 * time.Hour)).
		Build()
	require.NoError(t, err)

	hdrs := jws.NewHeaders()
	_ = hdrs.Set(jws.KeyIDKey, "test-key")

	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, privKey, jws.WithProtectedHeaders(hdrs)))
	require.NoError(t, err)

	authMiddleware := middleware.AuthMiddleware(validator)

	var extractedUserId string
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := middleware.GetUserFromContext(r.Context())
		if err == nil {
			extractedUserId = u.String()
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/v1/holdings", nil)
	req.Header.Set("Authorization", "Bearer "+string(signed))
	rec := httptest.NewRecorder()

	authMiddleware(nextHandler).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, userId.String(), extractedUserId)
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	validator := middleware.NewStaticJWTValidator(jwk.NewSet(), "", "")
	authMiddleware := middleware.AuthMiddleware(validator)

	req := httptest.NewRequest("GET", "/api/v1/holdings", nil)
	rec := httptest.NewRecorder()

	authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json")
}

func TestAuthMiddleware_ExpiredToken(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	key, err := jwk.FromRaw(privKey.Public())
	require.NoError(t, err)
	_ = key.Set(jwk.KeyIDKey, "test-key")

	keySet := jwk.NewSet()
	_ = keySet.AddKey(key)

	validator := middleware.NewStaticJWTValidator(keySet, "test-iss", "authenticated")

	tok, err := jwt.NewBuilder().
		Issuer("test-iss").
		Audience([]string{"authenticated"}).
		Subject(uuid.New().String()).
		IssuedAt(time.Now().Add(-2 * time.Hour)).
		Expiration(time.Now().Add(-1 * time.Hour)).
		Build()
	require.NoError(t, err)

	hdrs := jws.NewHeaders()
	_ = hdrs.Set(jws.KeyIDKey, "test-key")

	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, privKey, jws.WithProtectedHeaders(hdrs)))
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/v1/holdings", nil)
	req.Header.Set("Authorization", "Bearer "+string(signed))
	rec := httptest.NewRecorder()

	middleware.AuthMiddleware(validator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
