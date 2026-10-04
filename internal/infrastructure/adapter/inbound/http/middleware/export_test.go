package middleware

import (
	"context"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
)

// SetKeyFetchTiming changes the JWKS re-fetch throttle so tests needn't wait out the real one.
func SetKeyFetchTiming(v *SupabaseJWTValidator, interval, retryBackoff time.Duration) {
	v.fetchInterval, v.retryBackoff = interval, retryBackoff
}

// SetKeyFetcher replaces how the JWKS is re-fetched (e.g. with one that panics).
func SetKeyFetcher(v *SupabaseJWTValidator, fetch func(context.Context) (jwk.Set, error)) {
	v.refresh = fetch
}
