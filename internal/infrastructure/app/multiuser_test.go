package app_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/app"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// End-to-end through the real router, auth middleware, services and MongoDB: the only fake is the
// identity provider (a local JWKS standing in for Supabase), so tokens are verified exactly as in prod.

const (
	e2eIssuer = "https://e2e.supabase.co/auth/v1"
	e2eKid    = "e2e-key"
)

type e2e struct {
	t       *testing.T
	handler http.Handler
	priv    *rsa.PrivateKey
	dbName  string
}

func newE2E(t *testing.T, devUserID string) *e2e {
	t.Helper()
	cfg := testConfig(t)
	cfg.MongoDBName = "test_e2e_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	cfg.DevUserID = devUserID

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pub, err := jwk.FromRaw(priv.Public())
	require.NoError(t, err)
	require.NoError(t, pub.Set(jwk.KeyIDKey, e2eKid))
	require.NoError(t, pub.Set(jwk.AlgorithmKey, jwa.RS256))
	set := jwk.NewSet()
	require.NoError(t, set.AddKey(pub))
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(jwks.Close)

	cfg.JWKSetURI = jwks.URL
	cfg.AuthIssuer = e2eIssuer
	cfg.AuthAudience = "authenticated"

	ctx, cancel := context.WithCancel(context.Background())
	a, err := app.BuildApp(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		a.Cleanup()
		cancel()
		client, err := mongo.Connect(options.Client().ApplyURI(os.Getenv("MONGO_TEST_URI")))
		if err == nil {
			_ = client.Database(cfg.MongoDBName).Drop(context.Background())
			_ = client.Disconnect(context.Background())
		}
	})
	return &e2e{t: t, handler: a.Handler, priv: priv, dbName: cfg.MongoDBName}
}

// collection gives direct access to the app's MongoDB, for setups too slow to do through the API.
func (e *e2e) collection(name string) *mongo.Collection {
	e.t.Helper()
	client, err := mongo.Connect(options.Client().ApplyURI(os.Getenv("MONGO_TEST_URI")))
	require.NoError(e.t, err)
	e.t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	return client.Database(e.dbName).Collection(name)
}

// token signs a Supabase-shaped access token; edit tweaks it to build invalid ones.
func (e *e2e) token(sub uuid.UUID, edit func(*jwt.Builder) *jwt.Builder) string {
	e.t.Helper()
	b := jwt.NewBuilder().
		Issuer(e2eIssuer).
		Audience([]string{"authenticated"}).
		Subject(sub.String()).
		IssuedAt(time.Now()).
		Expiration(time.Now().Add(time.Hour)).
		Claim("role", "authenticated")
	if edit != nil {
		b = edit(b)
	}
	tok, err := b.Build()
	require.NoError(e.t, err)
	hdrs := jws.NewHeaders()
	require.NoError(e.t, hdrs.Set(jws.KeyIDKey, e2eKid))
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, e.priv, jws.WithProtectedHeaders(hdrs)))
	require.NoError(e.t, err)
	return string(signed)
}

// do sends a request as the bearer of token ("" = no Authorization header) and decodes the JSON reply into out.
func (e *e2e) do(token, method, path string, body any, out any) int {
	e.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(e.t, err)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if out != nil && rec.Body.Len() > 0 {
		require.NoError(e.t, json.Unmarshal(rec.Body.Bytes(), out), rec.Body.String())
	}
	return rec.Code
}

type holdingRes struct {
	Id         string  `json:"id"`
	Name       string  `json:"name"`
	AssetClass string  `json:"assetClass"`
	Platform   string  `json:"platform"`
	ValueUsd   float64 `json:"valueUsd"`
}

type summaryRes struct {
	NetWorth      struct{ Usd float64 } `json:"netWorth"`
	HoldingsCount int                   `json:"holdingsCount"`
	ByAssetClass  []struct {
		AssetClass string  `json:"assetClass"`
		ValueUsd   float64 `json:"valueUsd"`
	} `json:"byAssetClass"`
	ByPlatform []struct {
		Name     string  `json:"name"`
		ValueUsd float64 `json:"valueUsd"`
	} `json:"byPlatform"`
}

func (e *e2e) create(token, name, class, platform string, value float64) holdingRes {
	e.t.Helper()
	var h holdingRes
	code := e.do(token, "POST", "/api/v1/holdings", map[string]any{
		"name": name, "assetClass": class, "platform": platform, "valueUsd": value,
	}, &h)
	require.Equal(e.t, http.StatusCreated, code)
	return h
}

func (e *e2e) holdingNames(token string) []string {
	e.t.Helper()
	var list []holdingRes
	require.Equal(e.t, http.StatusOK, e.do(token, "GET", "/api/v1/holdings", nil, &list))
	names := make([]string, len(list))
	for i, h := range list {
		names[i] = h.Name
	}
	sort.Strings(names)
	return names
}

func (e *e2e) platformNames(token string) []string {
	e.t.Helper()
	var list []struct{ Name string }
	require.Equal(e.t, http.StatusOK, e.do(token, "GET", "/api/v1/platforms", nil, &list))
	names := make([]string, len(list))
	for i, p := range list {
		names[i] = p.Name
	}
	return names
}

func (e *e2e) summary(token string) summaryRes {
	e.t.Helper()
	var s summaryRes
	require.Equal(e.t, http.StatusOK, e.do(token, "GET", "/api/v1/wealth/summary", nil, &s))
	return s
}

func TestMultiUser_EachAccountOnlySeesAndChangesItsOwnData(t *testing.T) {
	e := newE2E(t, "")
	alice, bob := uuid.New(), uuid.New()
	asAlice, asBob := e.token(alice, nil), e.token(bob, nil)

	// Both use a "Binance" platform: each gets their own, with their own spelling.
	aliceBTC := e.create(asAlice, "BTC", "Crypto", "Binance", 1000)
	e.create(asAlice, "Savings", "Cash", "Bank A", 500)
	e.create(asBob, "ETH", "Crypto", "binance", 300)
	e.create(asBob, "Gold bar", "Commodity", "Vault", 200)

	assert.Equal(t, []string{"BTC", "Savings"}, e.holdingNames(asAlice))
	assert.Equal(t, []string{"ETH", "Gold bar"}, e.holdingNames(asBob))
	assert.Equal(t, []string{"Bank A", "Binance"}, e.platformNames(asAlice))
	assert.Equal(t, []string{"binance", "Vault"}, e.platformNames(asBob))

	var aliceClasses, bobClasses struct{ InUse []string }
	require.Equal(t, http.StatusOK, e.do(asAlice, "GET", "/api/v1/asset-classes", nil, &aliceClasses))
	require.Equal(t, http.StatusOK, e.do(asBob, "GET", "/api/v1/asset-classes", nil, &bobClasses))
	assert.ElementsMatch(t, []string{"Crypto", "Cash"}, aliceClasses.InUse)
	assert.ElementsMatch(t, []string{"Crypto", "Commodity"}, bobClasses.InUse)

	aliceSummary, bobSummary := e.summary(asAlice), e.summary(asBob)
	assert.Equal(t, 1500.0, aliceSummary.NetWorth.Usd)
	assert.Equal(t, 2, aliceSummary.HoldingsCount)
	assert.Len(t, aliceSummary.ByPlatform, 2)
	assert.Equal(t, 500.0, bobSummary.NetWorth.Usd)
	assert.Equal(t, 2, bobSummary.HoldingsCount)
	assert.Equal(t, "binance", bobSummary.ByPlatform[0].Name)

	var aliceEstimate, bobEstimate struct{ PrincipalUsd float64 }
	query := "/api/v1/wealth/estimate?contribution=100&yieldPct=5&years=1"
	require.Equal(t, http.StatusOK, e.do(asAlice, "GET", query, nil, &aliceEstimate))
	require.Equal(t, http.StatusOK, e.do(asBob, "GET", query, nil, &bobEstimate))
	assert.Equal(t, 1500.0, aliceEstimate.PrincipalUsd)
	assert.Equal(t, 500.0, bobEstimate.PrincipalUsd)

	// Snapshots: per account, and two accounts may snapshot the very same second.
	var aliceSnap, bobSnap struct{ TotalValueUsd float64 }
	require.Equal(t, http.StatusCreated, e.do(asAlice, "POST", "/api/v1/wealth/snapshots", nil, &aliceSnap))
	require.Equal(t, http.StatusCreated, e.do(asBob, "POST", "/api/v1/wealth/snapshots", nil, &bobSnap))
	assert.Equal(t, 1500.0, aliceSnap.TotalValueUsd)
	assert.Equal(t, 500.0, bobSnap.TotalValueUsd)
	var aliceSnaps, bobSnaps []struct{ TotalValueUsd float64 }
	require.Equal(t, http.StatusOK, e.do(asAlice, "GET", "/api/v1/wealth/snapshots", nil, &aliceSnaps))
	require.Equal(t, http.StatusOK, e.do(asBob, "GET", "/api/v1/wealth/snapshots", nil, &bobSnaps))
	require.Len(t, aliceSnaps, 1)
	require.Len(t, bobSnaps, 1)
	assert.Equal(t, 1500.0, aliceSnaps[0].TotalValueUsd)
	assert.Equal(t, 500.0, bobSnaps[0].TotalValueUsd)

	// Bob can't delete Alice's holding, even knowing its id: indistinguishable from a missing one.
	assert.Equal(t, http.StatusNotFound, e.do(asBob, "DELETE", "/api/v1/holdings/"+aliceBTC.Id, nil, nil))
	assert.Equal(t, []string{"BTC", "Savings"}, e.holdingNames(asAlice))

	// The caller is only ever the token's subject: user ids in the body or query are ignored.
	injected := e.create(asBob, "Sneaky", "Cash", "Vault", 1)
	assert.Equal(t, http.StatusCreated, e.do(asBob, "POST", "/api/v1/holdings", map[string]any{
		"name": "Planted", "assetClass": "Cash", "platform": "Vault", "valueUsd": 1,
		"userId": alice.String(), "user_id": alice.String(),
	}, nil))
	assert.Equal(t, []string{"BTC", "Savings"}, e.holdingNames(asAlice))
	var viaQuery []holdingRes
	require.Equal(t, http.StatusOK, e.do(asBob, "GET", "/api/v1/holdings?userId="+alice.String(), nil, &viaQuery))
	assert.Len(t, viaQuery, 4, "Bob's own four holdings")
	assert.Equal(t, http.StatusNoContent, e.do(asBob, "DELETE", "/api/v1/holdings/"+injected.Id, nil, nil))

	// Alice deleting her own holding doesn't touch Bob's same-named platform.
	assert.Equal(t, http.StatusNoContent, e.do(asAlice, "DELETE", "/api/v1/holdings/"+aliceBTC.Id, nil, nil))
	assert.Equal(t, []string{"Bank A"}, e.platformNames(asAlice))
	assert.Equal(t, []string{"binance", "Vault"}, e.platformNames(asBob))
	assert.Equal(t, 500.0, e.summary(asAlice).NetWorth.Usd)
	assert.Equal(t, 501.0, e.summary(asBob).NetWorth.Usd)
}

func TestMultiUser_EditsAndSnapshotDeletesOnlyReachTheOwner(t *testing.T) {
	e := newE2E(t, "")
	asAlice, asBob := e.token(uuid.New(), nil), e.token(uuid.New(), nil)
	btc := e.create(asAlice, "BTC", "Crypto", "Binance", 1000)
	e.create(asAlice, "ETH", "Crypto", "Ledger", 10)
	e.create(asBob, "ETH", "Crypto", "Ledger", 1)

	// Alice moves BTC to her Ledger (spelled as she spells it) and updates its value; Binance is gone.
	var edited holdingRes
	require.Equal(t, http.StatusOK, e.do(asAlice, "PATCH", "/api/v1/holdings/"+btc.Id, map[string]any{"platform": "ledger", "valueUsd": 1200}, &edited))
	assert.Equal(t, "Ledger", edited.Platform)
	assert.Equal(t, 1200.0, edited.ValueUsd)
	assert.Equal(t, []string{"Ledger"}, e.platformNames(asAlice))
	assert.Equal(t, 1210.0, e.summary(asAlice).NetWorth.Usd)

	// Bob can't edit her holding or delete her snapshot, even knowing their ids.
	var snap struct{ Id string }
	require.Equal(t, http.StatusCreated, e.do(asAlice, "POST", "/api/v1/wealth/snapshots", nil, &snap))
	assert.Equal(t, http.StatusNotFound, e.do(asBob, "PATCH", "/api/v1/holdings/"+btc.Id, map[string]any{"valueUsd": 1}, nil))
	assert.Equal(t, http.StatusNotFound, e.do(asBob, "DELETE", "/api/v1/wealth/snapshots/"+snap.Id, nil, nil))
	assert.Equal(t, 1210.0, e.summary(asAlice).NetWorth.Usd)
	assert.Equal(t, 1.0, e.summary(asBob).NetWorth.Usd)
	var snaps []struct{ Id string }
	require.Equal(t, http.StatusOK, e.do(asAlice, "GET", "/api/v1/wealth/snapshots", nil, &snaps))
	require.Len(t, snaps, 1)

	assert.Equal(t, http.StatusNoContent, e.do(asAlice, "DELETE", "/api/v1/wealth/snapshots/"+snap.Id, nil, nil))
	require.Equal(t, http.StatusOK, e.do(asAlice, "GET", "/api/v1/wealth/snapshots", nil, &snaps))
	assert.Empty(t, snaps)
}

func TestMultiUser_RejectsAnythingButAValidTokenForThisProject(t *testing.T) {
	e := newE2E(t, "")
	user := uuid.New()

	other := &e2e{t: t}
	var err error
	other.priv, err = rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	for name, token := range map[string]string{
		"no token":              "",
		"garbage":               "not-a-jwt",
		"signed by another key": other.token(user, nil),
		"another project":       e.token(user, func(b *jwt.Builder) *jwt.Builder { return b.Issuer("https://evil.supabase.co/auth/v1") }),
		"anon key audience":     e.token(user, func(b *jwt.Builder) *jwt.Builder { return b.Audience([]string{"anon"}) }),
		"expired":               e.token(user, func(b *jwt.Builder) *jwt.Builder { return b.Expiration(time.Now().Add(-time.Hour)) }),
		"anonymous sign-in":     e.token(user, func(b *jwt.Builder) *jwt.Builder { return b.Claim("is_anonymous", true) }),
		"subject not a user":    e.token(user, func(b *jwt.Builder) *jwt.Builder { return b.Subject("service_role") }),
	} {
		t.Run(name, func(t *testing.T) {
			for _, path := range []string{"/api/v1/holdings", "/api/v1/wealth/summary", "/api/v1/wealth/snapshots"} {
				assert.Equal(t, http.StatusUnauthorized, e.do(token, "GET", path, nil, nil), path)
			}
			assert.Equal(t, http.StatusUnauthorized, e.do(token, "POST", "/api/v1/holdings", map[string]any{
				"name": "x", "assetClass": "Cash", "platform": "Bank", "valueUsd": 1,
			}, nil))
		})
	}
}

func TestMultiUser_DevModeStillKeepsSignedInAccountsApart(t *testing.T) {
	devUser := uuid.New()
	e := newE2E(t, devUser.String())
	alice := e.token(uuid.New(), nil)

	e.create("", "Dev cash", "Cash", "Local", 10) // "Skip login (dev)": no token at all
	e.create(alice, "Alice cash", "Cash", "Bank", 20)

	assert.Equal(t, []string{"Dev cash"}, e.holdingNames(""))
	assert.Equal(t, []string{"Alice cash"}, e.holdingNames(alice))
	assert.Equal(t, http.StatusUnauthorized, e.do("forged", "GET", "/api/v1/holdings", nil, nil),
		"a bad token is rejected, not downgraded to the dev user")
}

func TestMultiUser_HoldingCapHoldsUnderConcurrentCreates(t *testing.T) {
	e := newE2E(t, "")
	user := uuid.New()
	token := e.token(user, nil)

	// Just under the cap, written straight to MongoDB: 990 POSTs would only slow the test down.
	const prefilled = model.MaxHoldingsPerUser - 10
	now := time.Now().UTC()
	docs := make([]any, prefilled)
	for i := range docs {
		docs[i] = bson.M{
			"_id": uuid.NewString(), "user_id": user.String(), "name": fmt.Sprintf("h%d", i), "asset_class": "Cash",
			"platform_name": "Bank", "value_usd": "1.00", "created_at": now, "updated_at": now,
		}
	}
	_, err := e.collection("holdings").InsertMany(context.Background(), docs)
	require.NoError(t, err)

	// 30 creates at once for 10 free slots: every one may pass the first check before any insert lands.
	const burst = 30
	codes := make([]int, burst)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/api/v1/holdings", strings.NewReader(
				fmt.Sprintf(`{"name":"burst %d","assetClass":"Cash","platform":"Bank","valueUsd":1}`, i)))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			e.handler.ServeHTTP(rec, req)
			codes[i] = rec.Code
		}(i)
	}
	wg.Wait()

	created := 0
	for _, code := range codes {
		require.Contains(t, []int{http.StatusCreated, http.StatusConflict}, code)
		if code == http.StatusCreated {
			created++
		}
	}
	var list []holdingRes
	require.Equal(t, http.StatusOK, e.do(token, "GET", "/api/v1/holdings", nil, &list))
	assert.LessOrEqual(t, len(list), model.MaxHoldingsPerUser, "never above the cap")
	assert.Equal(t, prefilled+created, len(list), "every 201 stayed, every 409 left nothing behind")
}
