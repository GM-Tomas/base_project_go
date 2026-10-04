package middleware

import "time"

// SetKeyFetchTiming shortens the JWKS re-fetch throttle so tests needn't wait out the real one.
func SetKeyFetchTiming(v *SupabaseJWTValidator, interval, retryBackoff time.Duration) {
	v.fetchInterval, v.retryBackoff = interval, retryBackoff
}
