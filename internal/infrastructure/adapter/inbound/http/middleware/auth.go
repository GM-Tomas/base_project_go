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

// ErrSigningKeysUnavailable means the JWKS couldn't be fetched. The token may well be valid, so it's
// answered with 503 (our problem) rather than 401, which makes the frontend sign the user out.
var ErrSigningKeysUnavailable = errors.New("signing keys unavailable")

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

// DevValidator makes requests that carry no token at all act as a fixed user, so "Skip login (dev)" and
// Swagger work offline. A token that is sent is still verified by Tokens: signed-in accounts keep their
// own data in dev mode too, instead of all collapsing into one user. Local only: config refuses it on Vercel.
type DevValidator struct {
	UserId model.UserId
	Tokens JWTValidator
}

func (v DevValidator) ValidateToken(ctx context.Context, tokenStr string) (model.UserId, error) {
	if v.Tokens == nil {
		return model.UserId{}, errors.New("dev mode has no token validator")
	}
	return v.Tokens.ValidateToken(ctx, tokenStr)
}

func (v *SupabaseJWTValidator) ValidateToken(ctx context.Context, tokenStr string) (model.UserId, error) {
	// No keys means no verification is possible: reject, never fall back to an unverified parse.
	keySet, err := v.keys(ctx)
	if err != nil {
		return model.UserId{}, fmt.Errorf("%w: fetching JWKS: %v", ErrSigningKeysUnavailable, err)
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

	// Anonymous sign-ins are off in Supabase; refuse their tokens anyway in case that setting ever flips.
	if anon, ok := tok.Get("is_anonymous"); ok && anon == true {
		return model.UserId{}, errors.New("anonymous sessions are not allowed")
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
				// Dev mode only: no token at all is the fixed local user ("Skip login", Swagger).
				if dev, ok := validator.(DevValidator); ok {
					next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), dev.UserId)))
					return
				}
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
				if errors.Is(err, ErrSigningKeysUnavailable) {
					WriteProblem(w, r, http.StatusServiceUnavailable, "auth-unavailable", "Service Unavailable",
						"Your sign-in can't be verified right now. Please try again shortly.", nil)
					return
				}
				WriteUnauthorized(w, r, "Missing or invalid access token")
				return
			}

			ctx := WithUser(r.Context(), userId)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
