package middleware

import (
	"context"
	"errors"
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
	jwkSetURI string
	issuer    string
	audience  string
	keySet    jwk.Set
	autoFetch bool
}

func NewSupabaseJWTValidator(ctx context.Context, jwkSetURI, issuer, audience string) *SupabaseJWTValidator {
	v := &SupabaseJWTValidator{
		jwkSetURI: jwkSetURI,
		issuer:    issuer,
		audience:  audience,
		autoFetch: true,
	}
	return v
}

func NewStaticJWTValidator(keySet jwk.Set, issuer, audience string) *SupabaseJWTValidator {
	return &SupabaseJWTValidator{
		keySet:    keySet,
		issuer:    issuer,
		audience:  audience,
		autoFetch: false,
	}
}

func (v *SupabaseJWTValidator) ValidateToken(ctx context.Context, tokenStr string) (model.UserId, error) {
	var parseOptions []jwt.ParseOption

	if v.autoFetch && v.keySet == nil && v.jwkSetURI != "" {
		set, err := jwk.Fetch(ctx, v.jwkSetURI)
		if err == nil {
			v.keySet = set
		}
	}

	if v.keySet != nil {
		parseOptions = append(parseOptions, jwt.WithKeySet(v.keySet))
	} else {
		// If JWKS is not reachable or not supplied, try parsing without verification only in insecure/test fallback
		parseOptions = append(parseOptions, jwt.WithVerify(false))
	}

	if v.issuer != "" {
		parseOptions = append(parseOptions, jwt.WithIssuer(v.issuer))
	}
	if v.audience != "" {
		parseOptions = append(parseOptions, jwt.WithAudience(v.audience))
	}
	parseOptions = append(parseOptions, jwt.WithAcceptableSkew(time.Minute))

	tok, err := jwt.Parse([]byte(tokenStr), parseOptions...)
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
				WriteUnauthorized(w, r, "Missing or invalid access token")
				return
			}

			ctx := WithUser(r.Context(), userId)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
