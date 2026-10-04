package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type fetchOutcome struct {
	set jwk.Set
	err error
}

// fakeJWKS answers each fetch with the next outcome the test sends, blocking until it does.
type fakeJWKS struct {
	calls    atomic.Int32
	outcomes chan fetchOutcome
}

func (f *fakeJWKS) fetch(ctx context.Context) (jwk.Set, error) {
	f.calls.Add(1)
	select {
	case o := <-f.outcomes:
		return o.set, o.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// newTestJWKSCache waits on other requests' refreshes for as long as given, so a test sees whether a
// request waited (a short ctx deadline on it) or not.
func newTestJWKSCache(staleWait time.Duration) (*jwksCache, *fakeJWKS, *fakeClock) {
	f := &fakeJWKS{outcomes: make(chan fetchOutcome)}
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	c := newJWKSCache(f.fetch)
	c.now = clock.Now
	c.staleWait = staleWait
	return c, f, clock
}

func keySet(t *testing.T, kid string) jwk.Set {
	t.Helper()
	key, err := jwk.FromRaw([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	require.NoError(t, key.Set(jwk.KeyIDKey, kid))
	set := jwk.NewSet()
	require.NoError(t, set.AddKey(key))
	return set
}

func answer(f *fakeJWKS, o fetchOutcome) { go func() { f.outcomes <- o }() }

func TestJWKSCache_OneFetchServesEveryoneAndOutlivesHangUps(t *testing.T) {
	c, f, _ := newTestJWKSCache(time.Hour)
	keys := keySet(t, "a")

	// The first request starts the fetch, and its client hangs up: the fetch still completes.
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	hungUp := make(chan error)
	go func() {
		_, err := c.Get(gone)
		hungUp <- err
	}()
	require.Eventually(t, func() bool { return f.calls.Load() == 1 }, 5*time.Second, time.Millisecond)

	var wg sync.WaitGroup
	got := make([]jwk.Set, 5)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], _ = c.Get(context.Background())
		}()
	}
	f.outcomes <- fetchOutcome{set: keys}
	wg.Wait()

	assert.ErrorIs(t, <-hungUp, context.Canceled)
	for _, set := range got {
		assert.Equal(t, keys, set)
	}
	assert.EqualValues(t, 1, f.calls.Load(), "one fetch for everyone")
}

// Regression: the previous cache (jwk.Cache) kept a failed first fetch as "fetched, no keys", so every
// request got a 503 until its next scheduled refresh, 15 to 30 minutes later.
func TestJWKSCache_AFailedFirstFetchIsRetriedSoon(t *testing.T) {
	c, f, clock := newTestJWKSCache(time.Hour)
	ctx := context.Background()

	answer(f, fetchOutcome{err: errors.New("supabase is down")})
	_, err := c.Get(ctx)
	assert.EqualError(t, err, "supabase is down")

	_, err = c.Get(ctx)
	assert.EqualError(t, err, "supabase is down", "answered without asking Supabase again right away")
	assert.EqualValues(t, 1, f.calls.Load())

	clock.Advance(jwksRetryAfter)
	keys := keySet(t, "a")
	answer(f, fetchOutcome{set: keys})
	got, err := c.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, keys, got)
	assert.EqualValues(t, 2, f.calls.Load())
}

func TestJWKSCache_RefetchesKeysOlderThanMaxAge(t *testing.T) {
	c, f, clock := newTestJWKSCache(time.Hour)
	ctx := context.Background()
	before, after := keySet(t, "before"), keySet(t, "after")

	answer(f, fetchOutcome{set: before})
	got, err := c.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, got)

	clock.Advance(jwksMaxAge - time.Second)
	got, _ = c.Get(ctx)
	assert.Equal(t, before, got)
	assert.EqualValues(t, 1, f.calls.Load())

	// Older than maxAge: the request waits for the new keys rather than verify against old ones.
	clock.Advance(time.Second)
	answer(f, fetchOutcome{set: after})
	got, err = c.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, after, got)
	assert.EqualValues(t, 2, f.calls.Load())
}

func TestJWKSCache_KeepsTheLastKeysWhileSupabaseIsDown(t *testing.T) {
	c, f, clock := newTestJWKSCache(time.Hour)
	ctx := context.Background()
	old, fresh := keySet(t, "old"), keySet(t, "fresh")

	answer(f, fetchOutcome{set: old})
	_, err := c.Get(ctx)
	require.NoError(t, err)

	clock.Advance(jwksMaxAge)
	answer(f, fetchOutcome{err: errors.New("supabase is down")})
	got, err := c.Get(ctx)
	require.NoError(t, err, "a failed refresh isn't an outage while the keys are usable")
	assert.Equal(t, old, got)

	got, _ = c.Get(ctx)
	assert.Equal(t, old, got)
	assert.EqualValues(t, 2, f.calls.Load(), "no new attempt before retryAfter")

	// The next request tries again; meanwhile the others don't wait on a Supabase that isn't answering.
	clock.Advance(jwksRetryAfter)
	retried := make(chan jwk.Set)
	go func() {
		set, _ := c.Get(ctx)
		retried <- set
	}()
	require.Eventually(t, func() bool { return f.calls.Load() == 3 }, 5*time.Second, time.Millisecond)
	for range 3 {
		quick, cancel := context.WithTimeout(ctx, time.Second)
		got, err = c.Get(quick)
		cancel()
		require.NoError(t, err)
		assert.Equal(t, old, got)
	}

	f.outcomes <- fetchOutcome{set: fresh}
	assert.Equal(t, fresh, <-retried)
	got, _ = c.Get(ctx)
	assert.Equal(t, fresh, got)
	assert.EqualValues(t, 3, f.calls.Load())
}

func TestJWKSCache_StopsUsingKeysPastMaxStale(t *testing.T) {
	c, f, clock := newTestJWKSCache(time.Hour)
	ctx := context.Background()

	answer(f, fetchOutcome{set: keySet(t, "old")})
	_, err := c.Get(ctx)
	require.NoError(t, err)

	clock.Advance(jwksMaxAge)
	answer(f, fetchOutcome{err: errors.New("supabase is down")})
	_, err = c.Get(ctx)
	require.NoError(t, err)

	// Keys this old may have been revoked since: no answer rather than a possibly wrong one.
	clock.Advance(jwksMaxStale - jwksMaxAge)
	answer(f, fetchOutcome{err: errors.New("supabase is still down")})
	_, err = c.Get(ctx)
	assert.EqualError(t, err, "supabase is still down")
	_, err = c.Get(ctx)
	assert.EqualError(t, err, "supabase is still down")
	assert.EqualValues(t, 3, f.calls.Load())

	clock.Advance(jwksRetryAfter)
	fresh := keySet(t, "fresh")
	answer(f, fetchOutcome{set: fresh})
	got, err := c.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, fresh, got)
}

func TestJWKSCache_DuringARefreshOthersGetTheNewKeysIfTheyComeInTime(t *testing.T) {
	c, f, clock := newTestJWKSCache(time.Hour)
	ctx := context.Background()
	old, fresh := keySet(t, "old"), keySet(t, "fresh")

	answer(f, fetchOutcome{set: old})
	_, err := c.Get(ctx)
	require.NoError(t, err)

	clock.Advance(jwksMaxAge)
	refreshed := make(chan jwk.Set, 2)
	go func() {
		set, _ := c.Get(ctx)
		refreshed <- set
	}()
	require.Eventually(t, func() bool { return f.calls.Load() == 2 }, 5*time.Second, time.Millisecond)
	go func() {
		set, _ := c.Get(ctx)
		refreshed <- set
	}()
	select {
	case <-refreshed:
		t.Fatal("answered before the refresh under way finished")
	case <-time.After(50 * time.Millisecond):
	}

	f.outcomes <- fetchOutcome{set: fresh}
	assert.Equal(t, fresh, <-refreshed)
	assert.Equal(t, fresh, <-refreshed, "a key rotated in since the last fetch is in the new keys")
	assert.EqualValues(t, 2, f.calls.Load())
}

func TestJWKSCache_DuringASlowRefreshOthersUseTheCurrentKeys(t *testing.T) {
	c, f, clock := newTestJWKSCache(20 * time.Millisecond)
	ctx := context.Background()
	old, fresh := keySet(t, "old"), keySet(t, "fresh")

	answer(f, fetchOutcome{set: old})
	_, err := c.Get(ctx)
	require.NoError(t, err)

	clock.Advance(jwksMaxAge)
	refreshed := make(chan jwk.Set)
	go func() {
		set, _ := c.Get(ctx)
		refreshed <- set
	}()
	require.Eventually(t, func() bool { return f.calls.Load() == 2 }, 5*time.Second, time.Millisecond)

	// Far longer than staleWait, so only a request waiting on the refresh itself runs into it, even on a
	// loaded machine.
	patient, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	got, err := c.Get(patient)
	require.NoError(t, err, "not held up on a slow Supabase while the keys still work")
	assert.Equal(t, old, got)

	f.outcomes <- fetchOutcome{set: fresh}
	assert.Equal(t, fresh, <-refreshed)
}

func TestJWKSCache_SurvivesAPanickingFetch(t *testing.T) {
	keys := keySet(t, "a")
	var calls atomic.Int32
	c := newJWKSCache(func(context.Context) (jwk.Set, error) {
		if calls.Add(1) == 1 {
			panic("boom")
		}
		return keys, nil
	})
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	c.now = clock.Now

	_, err := c.Get(context.Background())
	assert.ErrorContains(t, err, "panicked: boom")

	clock.Advance(jwksRetryAfter)
	got, err := c.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, keys, got)
}

// brokenSet is a key set whose Len panics. (Embedded under another name: jwk.Set has a Set method.)
type anySet = jwk.Set
type brokenSet struct{ anySet }

func (brokenSet) Len() int { panic("broken set") }

func TestJWKSCache_SurvivesAKeySetThatPanics(t *testing.T) {
	keys := keySet(t, "a")
	var calls atomic.Int32
	c := newJWKSCache(func(context.Context) (jwk.Set, error) {
		if calls.Add(1) == 1 {
			return brokenSet{}, nil
		}
		return keys, nil
	})
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	c.now = clock.Now

	_, err := c.Get(context.Background())
	assert.ErrorContains(t, err, "panicked: broken set")

	clock.Advance(jwksRetryAfter)
	got, err := c.Get(context.Background())
	require.NoError(t, err, "the failed fetch was recorded, so the next one could start")
	assert.Equal(t, keys, got)
}

func TestJWKSCache_NoKeysIsAFailedFetch(t *testing.T) {
	c := newJWKSCache(func(context.Context) (jwk.Set, error) { return nil, nil })
	_, err := c.Get(context.Background())
	assert.Error(t, err)

	// An empty set doesn't replace good keys: tokens would all be rejected, signing everyone out.
	c, f, clock := newTestJWKSCache(time.Hour)
	ctx := context.Background()
	keys := keySet(t, "a")
	answer(f, fetchOutcome{set: keys})
	_, err = c.Get(ctx)
	require.NoError(t, err)

	clock.Advance(jwksMaxAge)
	answer(f, fetchOutcome{set: jwk.NewSet()})
	got, err := c.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, keys, got)

	clock.Advance(jwksMaxStale)
	answer(f, fetchOutcome{set: jwk.NewSet()})
	_, err = c.Get(ctx)
	assert.EqualError(t, err, "the JWKS has no keys")
}

func TestJWKSCache_AHungSupabaseHoldsTheFetchingRequestOnlyUntilTheTimeout(t *testing.T) {
	c, f, clock := newTestJWKSCache(time.Hour)
	c.fetchTimeout = 20 * time.Millisecond
	ctx := context.Background()

	start := time.Now()
	_, err := c.Get(ctx) // nobody answers: the fetch gives up at the timeout
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 5*time.Second)

	clock.Advance(jwksRetryAfter)
	keys := keySet(t, "a")
	answer(f, fetchOutcome{set: keys})
	got, err := c.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, keys, got)
}

func TestFetchJWKS(t *testing.T) {
	serve := func(status int, body string) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	fetch := func(url string) (jwk.Set, error) {
		return fetchJWKS(&http.Client{}, url)(context.Background())
	}

	set, err := fetch(serve(http.StatusOK, `{"keys":[{"kty":"oct","kid":"a","k":"MDEyMzQ1Njc4OWFiY2RlZg"}]}`))
	require.NoError(t, err)
	_, found := set.LookupKeyID("a")
	assert.True(t, found)

	for name, url := range map[string]string{
		"error status": serve(http.StatusInternalServerError, `{"keys":[]}`),
		"not a JWKS":   serve(http.StatusOK, `<html>maintenance</html>`),
		"too large":    serve(http.StatusOK, `{"keys":[],"pad":"`+strings.Repeat("a", jwksMaxBytes)+`"}`),
		"unreachable":  "http://127.0.0.1:1/jwks.json",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := fetch(url)
			assert.Error(t, err)
		})
	}
}
