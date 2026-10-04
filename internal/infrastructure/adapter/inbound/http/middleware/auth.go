package middleware

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

type JWTValidator interface {
	ValidateToken(ctx context.Context, tokenStr string) (model.UserId, error)
}

type SupabaseJWTValidator struct {
	keys     func(ctx context.Context) (jwk.Set, error)
	issuer   string
	audience string
}

// NewSupabaseJWTValidator verifies tokens against the Supabase JWKS (asymmetric signing keys).
// The set is cached and refreshed in the background, so key rotation is picked up without a restart.
func NewSupabaseJWTValidator(ctx context.Context, jwkSetURI, issuer, audience string) (*SupabaseJWTValidator, error) {
	cache := jwk.NewCache(ctx)
	err := cache.Register(jwkSetURI,
		jwk.WithMinRefreshInterval(15*time.Minute),
		jwk.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
	)
	if err != nil {
		return nil, err
	}
	return &SupabaseJWTValidator{
		keys:     func(ctx context.Context) (jwk.Set, error) { return cache.Get(ctx, jwkSetURI) },
		issuer:   issuer,
		audience: audience,
	}, nil
}

func NewStaticJWTValidator(keySet jwk.Set, issuer, audience string) *SupabaseJWTValidator {
	return &SupabaseJWTValidator{
		keys:     func(context.Context) (jwk.Set, error) { return keySet, nil },
		issuer:   issuer,
		audience: audience,
	}
}

// DevValidator accepts any token as a fixed user. Local only: config refuses it on Vercel.
type DevValidator struct{ UserId model.UserId }

func (v DevValidator) ValidateToken(context.Context, string) (model.UserId, error) {
	return v.UserId, nil
}

func (v *SupabaseJWTValidator) ValidateToken(ctx context.Context, tokenStr string) (model.UserId, error) {
	// No keys means no verification is possible: reject, never fall back to an unverified parse.
	keySet, err := v.keys(ctx)
	if err != nil {
		return model.UserId{}, fmt.Errorf("fetching JWKS: %w", err)
	}

	tok, err := jwt.Parse([]byte(tokenStr),
		jwt.WithKeySet(keySet),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithAcceptableSkew(time.Minute),
	)
	if err != nil {
		return model.UserId{}, err
	}

	sub := tok.Subject()
	if sub == "" {
		return model.UserId{}, errors.New("jwt missing sub claim")
	}

	userId, err := model.ParseUserId(sub)
	if err != nil {
		return model.UserId{}, err
	}

	return userId, nil
}

func AuthMiddleware(validator JWTValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				WriteUnauthorized(w, r, "Missing or invalid access token")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				WriteUnauthorized(w, r, "Missing or invalid access token")
				return
			}

			tokenStr := strings.TrimSpace(parts[1])
			if tokenStr == "" {
				WriteUnauthorized(w, r, "Missing or invalid access token")
				return
			}

			userId, err := validator.ValidateToken(r.Context(), tokenStr)
			if err != nil {
				log.Printf("auth: rejected token [traceId=%s]: %v", GetTraceID(r.Context()), err)
				WriteUnauthorized(w, r, "Missing or invalid access token")
				return
			}

			ctx := WithUser(r.Context(), userId)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
