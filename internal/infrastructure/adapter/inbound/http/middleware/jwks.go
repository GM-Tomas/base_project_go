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
	// jwksMaxAge is how long a fetched key set is trusted before it's fetched again. Supabase's edge caches
	// the endpoint for another 10 minutes, and its docs ask apps not to keep the keys longer than that, so
	// rotations and revocations reach the API within their 20-minute window (see the README).
	jwksMaxAge = 10 * time.Minute
	// jwksRetryAfter spaces out the attempts while Supabase can't be reached.
	jwksRetryAfter   = 5 * time.Second
	jwksFetchTimeout = 10 * time.Second
	jwksMaxBytes     = 1 << 20 // a real JWKS is a few KB
)

// jwksCache keeps the Supabase signing keys in memory. Once they're older than maxAge they are fetched
// again, by one fetch that every request needing keys at the time waits on. Nothing a client sends (an
// unknown key id, say) can trigger a fetch. If one fails, the keys from the last fetch that worked stay in
// use, and later attempts, at most one per retryAfter, run in the background while those keys are served.
// Without any keys (a cold start while Supabase is down) requests fail until an attempt works.
type jwksCache struct {
	fetch      func(ctx context.Context) (jwk.Set, error)
	maxAge     time.Duration
	retryAfter time.Duration
	now        func() time.Time

	mu        sync.Mutex
	set       jwk.Set
	fetchedAt time.Time
	failedAt  time.Time // when the last attempt failed; zero once one works
	lastErr   error
	inflight  chan struct{} // closed when the running fetch is done; nil while none is running
}

func newJWKSCache(fetch func(ctx context.Context) (jwk.Set, error)) *jwksCache {
	return &jwksCache{fetch: fetch, maxAge: jwksMaxAge, retryAfter: jwksRetryAfter, now: time.Now}
}

func (c *jwksCache) Get(ctx context.Context) (jwk.Set, error) {
	c.mu.Lock()
	now := c.now()
	retryDue := c.failedAt.IsZero() || now.Sub(c.failedAt) >= c.retryAfter
	switch {
	case c.set != nil && now.Sub(c.fetchedAt) < c.maxAge:
		// Fresh keys: the usual case.
	case c.set != nil && !c.failedAt.IsZero():
		// Supabase didn't answer last time: keep using the keys we have rather than wait on it again. The
		// next attempt runs in the background; if the host freezes it, these keys still work meanwhile.
		if c.inflight == nil && retryDue {
			done := c.beginFetchLocked()
			go c.runFetch(done)
		}
	case c.inflight != nil:
		done := c.inflight
		c.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return c.current()
	case !retryDue:
		// No keys, and the last attempt failed moments ago.
		err := c.lastErr
		c.mu.Unlock()
		return nil, err
	default:
		done := c.beginFetchLocked()
		c.mu.Unlock()
		// Run by this request, for everyone waiting on it: detached from its cancellation (its client may
		// hang up), but not left to a background goroutine, which a serverless host can freeze mid-fetch.
		c.runFetch(done)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return c.current()
	}
	set := c.set
	c.mu.Unlock()
	return set, nil
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
		} else if err == nil && set == nil {
			err = errors.New("fetching JWKS returned no key set")
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
	ctx, cancel := context.WithTimeout(context.Background(), jwksFetchTimeout)
	defer cancel()
	set, err = c.fetch(ctx)
}

// current returns the keys after a fetch: new ones, or if it failed, the ones from before (if any).
func (c *jwksCache) current() (jwk.Set, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.set == nil {
		return nil, c.lastErr
	}
	return c.set, nil
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
