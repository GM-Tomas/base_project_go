package middleware

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

// JWTValidator resolves the caller from a bearer token. A request without an Authorization header at all
// is validated as the empty token, which only dev mode accepts.
type JWTValidator interface {
	ValidateToken(ctx context.Context, tokenStr string) (model.UserId, error)
}

const statusClientClosedRequest = 499

var (
	ErrMissingToken = errors.New("missing access token")
	// ErrSigningKeysUnavailable means the JWKS couldn't be fetched. The token may well be valid, so it's
	// answered with 503 (our problem) rather than 401, which makes the frontend sign the user out.
	ErrSigningKeysUnavailable = errors.New("signing keys unavailable")
)

// A token whose key the cached JWKS lacks (just rotated in) makes the validator re-fetch it, at most once
// per keyFetchInterval after a successful fetch (made-up key ids can't make the API hammer Supabase; a key
// missing from a fetch that recent is truly unknown: 401), and once per keyFetchRetryBackoff after a failed
// one (meanwhile the keys are "unavailable": 503).
const (
	keyFetchInterval     = 5 * time.Second
	keyFetchRetryBackoff = 5 * time.Second
)

var errKeyFetchAborted = errors.New("JWKS fetch aborted")

type SupabaseJWTValidator struct {
	keys     func(ctx context.Context) (jwk.Set, error)
	refresh  func(ctx context.Context) (jwk.Set, error) // re-fetches, bypassing the cache
	issuer   string
	audience string

	fetchInterval, retryBackoff time.Duration

	mu        sync.Mutex
	inFlight  *keyFetch // the re-fetch under way, which concurrent callers wait for
	nextFetch time.Time // no new re-fetch before this
	lastErr   error     // why the last re-fetch failed, if it did
}

type keyFetch struct {
	done chan struct{}
	set  jwk.Set
	err  error
}

// NewSupabaseJWTValidator verifies tokens against the Supabase JWKS (asymmetric signing keys).
// The set is cached and refreshed in the background; a token signed by a key the cache doesn't know yet
// (just rotated in) makes it re-fetch, so the token isn't rejected and its user signed out.
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
		keys:          func(ctx context.Context) (jwk.Set, error) { return cache.Get(ctx, jwkSetURI) },
		refresh:       func(ctx context.Context) (jwk.Set, error) { return cache.Refresh(ctx, jwkSetURI) },
		issuer:        issuer,
		audience:      audience,
		fetchInterval: keyFetchInterval,
		retryBackoff:  keyFetchRetryBackoff,
	}, nil
}

func NewStaticJWTValidator(keySet jwk.Set, issuer, audience string) *SupabaseJWTValidator {
	static := func(context.Context) (jwk.Set, error) { return keySet, nil }
	return &SupabaseJWTValidator{
		keys:          static,
		refresh:       static,
		issuer:        issuer,
		audience:      audience,
		fetchInterval: keyFetchInterval,
		retryBackoff:  keyFetchRetryBackoff,
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
	if err == nil && !hasKey(keySet, tokenStr) {
		keySet, err = v.refreshedKeys(ctx, keySet)
	}
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

// hasKey reports whether the token names a key the set has. A token that names none, or can't be parsed,
// counts as known: re-fetching keys can't help it, and verification rejects it anyway.
func hasKey(keySet jwk.Set, tokenStr string) bool {
	msg, err := jws.Parse([]byte(tokenStr))
	if err != nil || len(msg.Signatures()) == 0 {
		return true
	}
	kid := msg.Signatures()[0].ProtectedHeaders().KeyID()
	if kid == "" {
		return true
	}
	_, ok := keySet.LookupKeyID(kid)
	return ok
}

// refreshedKeys returns the key set to verify a token whose key the cached set lacks: a freshly fetched one
// when a re-fetch is due or already under way (concurrent callers share it, so none is judged against the
// stale set meanwhile). Otherwise the cached set, fetched moments ago, or the error that fetch failed with.
func (v *SupabaseJWTValidator) refreshedKeys(ctx context.Context, cached jwk.Set) (jwk.Set, error) {
	v.mu.Lock()
	if fetch := v.inFlight; fetch != nil {
		v.mu.Unlock()
		select {
		case <-fetch.done:
			return fetch.set, fetch.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if time.Now().Before(v.nextFetch) {
		err := v.lastErr
		v.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return cached, nil
	}
	fetch := &keyFetch{done: make(chan struct{}), err: errKeyFetchAborted}
	v.inFlight = fetch
	v.mu.Unlock()

	// Whatever happens to the fetch, even a panic, the waiters are released and the next one can start.
	defer func() {
		v.mu.Lock()
		v.inFlight = nil
		v.lastErr = fetch.err
		if fetch.err != nil {
			v.nextFetch = time.Now().Add(v.retryBackoff)
		} else {
			v.nextFetch = time.Now().Add(v.fetchInterval)
		}
		v.mu.Unlock()
		close(fetch.done)
	}()
	// Run by this request, for everyone waiting on it: detached from its cancellation (its client may hang
	// up), but not left to a background goroutine, which a serverless host can freeze mid-fetch.
	fetch.set, fetch.err = v.refresh(context.WithoutCancel(ctx))
	return fetch.set, fetch.err
}

func AuthMiddleware(validator JWTValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var tokenStr string
			if authHeader := r.Header.Get("Authorization"); authHeader != "" {
				parts := strings.SplitN(authHeader, " ", 2)
				if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
					WriteUnauthorized(w, r, "Missing or invalid access token")
					return
				}
				tokenStr = strings.TrimSpace(parts[1])
				if tokenStr == "" {
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
