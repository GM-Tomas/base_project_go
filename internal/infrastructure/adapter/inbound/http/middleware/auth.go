package middleware

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

// JWTValidator resolves the caller from a bearer token. A request that carries none (no Authorization
// header, or the docs' Basic credentials) is validated as the empty token, which only dev mode accepts.
type JWTValidator interface {
	ValidateToken(ctx context.Context, tokenStr string) (model.UserId, error)
}

const statusClientClosedRequest = 499

var (
	ErrMissingToken = errors.New("missing access token")
	// ErrSigningKeysUnavailable means there are no usable keys to verify with: the JWKS couldn't be
	// fetched, at all or for too long (see jwksCache). The token may well be valid, so it's answered with
	// 503 (our problem) rather than 401, which makes the frontend sign the user out.
	ErrSigningKeysUnavailable = errors.New("signing keys unavailable")
)

type SupabaseJWTValidator struct {
	keys     func(ctx context.Context) (jwk.Set, error)
	issuer   string
	audience string
}

// NewSupabaseJWTValidator verifies tokens against the Supabase JWKS (asymmetric signing keys), fetched
// on first use and kept as jwksCache describes, so key rotation is picked up without a restart (rotate
// through a standby key, see the README, so the new key is known before tokens use it).
func NewSupabaseJWTValidator(jwkSetURI, issuer, audience string) (*SupabaseJWTValidator, error) {
	if u, err := url.Parse(jwkSetURI); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("invalid JWKS URL %q", jwkSetURI)
	}
	return &SupabaseJWTValidator{
		keys:     newJWKSCache(fetchJWKS(&http.Client{}, jwkSetURI)).Get,
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

// DevValidator makes requests that carry no token (see JWTValidator) act as a fixed user, so "Skip login
// (dev)" and Swagger work offline. A token that is sent is still verified by Tokens: signed-in accounts keep their
// own data in dev mode too, instead of all collapsing into one user. Local only: config refuses it on Vercel.
type DevValidator struct {
	UserId model.UserId
	Tokens JWTValidator
}

func (v DevValidator) ValidateToken(ctx context.Context, tokenStr string) (model.UserId, error) {
	if tokenStr == "" {
		return v.UserId, nil
	}
	if v.Tokens == nil {
		return model.UserId{}, errors.New("dev mode has no token validator")
	}
	return v.Tokens.ValidateToken(ctx, tokenStr)
}

func (v *SupabaseJWTValidator) ValidateToken(ctx context.Context, tokenStr string) (model.UserId, error) {
	if tokenStr == "" {
		return model.UserId{}, ErrMissingToken
	}

	// No keys means no verification is possible: reject, never fall back to an unverified parse.
	keySet, err := v.keys(ctx)
	if err != nil {
		// A caller that hung up says nothing about the keys.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return model.UserId{}, ctxErr
		}
		return model.UserId{}, fmt.Errorf("%w: fetching JWKS: %w", ErrSigningKeysUnavailable, err)
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
			var tokenStr string
			if header := r.Header.Get("Authorization"); header != "" {
				scheme, credentials, _ := strings.Cut(header, " ")
				switch {
				case strings.EqualFold(scheme, "Bearer") && strings.TrimSpace(credentials) != "":
					tokenStr = strings.TrimSpace(credentials)
				case strings.EqualFold(scheme, "Basic"):
					// The docs' credentials, which browsers resend to the whole site: not a token, so none.
				default:
					WriteUnauthorized(w, r, "Missing or invalid access token")
					return
				}
			}

			userId, err := validator.ValidateToken(r.Context(), tokenStr)
			switch {
			case err == nil:
				next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), userId)))
			case r.Context().Err() != nil:
				// The client hung up mid-verification. Nobody reads the answer, but the access log does:
				// 499 (nginx's "client closed request") instead of a misleading 200 or a fake auth outage.
				w.WriteHeader(statusClientClosedRequest)
			case errors.Is(err, ErrMissingToken):
				WriteUnauthorized(w, r, "Missing or invalid access token")
			case errors.Is(err, ErrSigningKeysUnavailable):
				log.Printf("auth: can't verify token [traceId=%s]: %v", GetTraceID(r.Context()), err)
				WriteProblem(w, r, http.StatusServiceUnavailable, "auth-unavailable", "Service Unavailable",
					"Your sign-in can't be verified right now. Please try again shortly.", nil)
			default:
				log.Printf("auth: rejected token [traceId=%s]: %v", GetTraceID(r.Context()), err)
				WriteUnauthorized(w, r, "Missing or invalid access token")
			}
		})
	}
}
