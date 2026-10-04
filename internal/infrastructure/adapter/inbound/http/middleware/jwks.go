package middleware

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
)

const (
	// jwksMaxAge is how long a fetched key set is used before it's fetched again. Supabase's edge caches
	// the endpoint for another 10 minutes, and its docs ask apps not to keep the keys longer than that, so
	// rotations and revocations reach the API within their 20-minute window (see the README).
	jwksMaxAge = 10 * time.Minute
	// jwksMaxStale is how long keys are used at most, when fetching them again keeps failing: past it,
	// requests get a 503 until a fetch works, rather than keys that may have been revoked meanwhile.
	jwksMaxStale = 20 * time.Minute
	// jwksStaleWait is how long a request waits for the new keys another request is fetching when the
	// current ones are past jwksMaxAge but still usable.
	jwksStaleWait = time.Second
	// jwksRetryAfter spaces out the attempts while Supabase can't be reached.
	jwksRetryAfter   = 5 * time.Second
	jwksFetchTimeout = 5 * time.Second
	jwksMaxBytes     = 1 << 20 // a real JWKS is a few KB
)

// jwksCache keeps the Supabase signing keys in memory. Keys older than maxAge are fetched again by the
// next request that needs them, for everyone; nothing a client sends (an unknown key id, say) can
// trigger a fetch. Every fetch runs in a request, never in a background goroutine, which a serverless host
// freezes once the responses are sent. While a fetch is under way, requests that have no usable keys wait
// for it; the others use the current keys, after waiting up to staleWait for the new ones (unless the last
// attempt failed: Supabase isn't answering, so no point). Keys stay usable until maxStale, so a failed
// fetch costs nothing until then; past it, or before the first fetch works, requests fail.
type jwksCache struct {
	fetch        func(ctx context.Context) (jwk.Set, error)
	fetchTimeout time.Duration
	maxAge       time.Duration
	maxStale     time.Duration
	staleWait    time.Duration
	retryAfter   time.Duration
	now          func() time.Time

	mu        sync.Mutex
	set       jwk.Set
	fetchedAt time.Time
	failedAt  time.Time // when the last attempt failed; zero once one works
	lastErr   error
	inflight  chan struct{} // closed when the running fetch is done; nil while none is running
}

func newJWKSCache(fetch func(ctx context.Context) (jwk.Set, error)) *jwksCache {
	return &jwksCache{
		fetch:        fetch,
		fetchTimeout: jwksFetchTimeout,
		maxAge:       jwksMaxAge,
		maxStale:     jwksMaxStale,
		staleWait:    jwksStaleWait,
		retryAfter:   jwksRetryAfter,
		now:          time.Now,
	}
}

func (c *jwksCache) Get(ctx context.Context) (jwk.Set, error) {
	c.mu.Lock()
	now := c.now()
	if c.set != nil && now.Sub(c.fetchedAt) < c.maxAge {
		set := c.set
		c.mu.Unlock()
		return set, nil
	}
	usable := c.usableLocked(now)
	failing := !c.failedAt.IsZero()
	if c.inflight == nil && (!failing || now.Sub(c.failedAt) >= c.retryAfter) {
		done := c.beginFetchLocked()
		c.mu.Unlock()
		// Fetched by this request, for everyone: detached from its cancellation, since its client may hang up.
		c.runFetch(done)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return c.current()
	}
	done := c.inflight
	c.mu.Unlock()

	switch {
	case done == nil, usable && failing:
		// The last attempt failed (moments ago, or is being retried): answer with the keys there are, if
		// they're still usable, rather than wait on Supabase.
	case usable:
		// Another request is fetching: wait a moment for the new keys (a key just rotated in may need
		// them), but not on a slow Supabase while the current keys still work.
		timer := time.NewTimer(c.staleWait)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	default:
		select {
		case <-done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.current()
}

// usableLocked reports whether there are keys young enough to verify tokens with.
func (c *jwksCache) usableLocked(now time.Time) bool {
	return c.set != nil && now.Sub(c.fetchedAt) < c.maxStale
}

func (c *jwksCache) beginFetchLocked() chan struct{} {
	c.inflight = make(chan struct{})
	return c.inflight
}

// runFetch fetches the keys and records the outcome. Whatever happens, even a panic, the waiters on done
// are released and the next attempt can start.
func (c *jwksCache) runFetch(done chan struct{}) {
	var set jwk.Set
	var err error
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("fetching JWKS panicked: %v", p)
		} else if err == nil && (set == nil || set.Len() == 0) {
			// Not a usable answer (a project still on the legacy HS256 secret, or Supabase misbehaving):
			// keep the keys from before, if any, rather than reject every token with none.
			err = errors.New("the JWKS has no keys")
		}
		c.mu.Lock()
		if err == nil {
			c.set, c.fetchedAt, c.failedAt, c.lastErr = set, c.now(), time.Time{}, nil
		} else {
			c.failedAt, c.lastErr = c.now(), err
		}
		c.inflight = nil
		c.mu.Unlock()
		close(done)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), c.fetchTimeout)
	defer cancel()
	set, err = c.fetch(ctx)
}

// current returns the newest keys if they're still usable, or why there are none.
func (c *jwksCache) current() (jwk.Set, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.usableLocked(c.now()) {
		return c.set, nil
	}
	if c.lastErr != nil {
		return nil, c.lastErr
	}
	return nil, errors.New("no usable JWKS")
}

// fetchJWKS returns a fetch of the key set at url.
func fetchJWKS(client *http.Client, url string) func(ctx context.Context) (jwk.Set, error) {
	return func(ctx context.Context) (jwk.Set, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s: %s", url, res.Status)
		}
		body, err := io.ReadAll(io.LimitReader(res.Body, jwksMaxBytes))
		if err != nil {
			return nil, err
		}
		return jwk.Parse(body)
	}
}
