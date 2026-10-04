package middleware_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testIssuer   = "https://ref.supabase.co/auth/v1"
	testAudience = "authenticated"
	testKid      = "kid-1"
)

func newKey(t *testing.T) (*rsa.PrivateKey, jwk.Set) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pub, err := jwk.FromRaw(priv.Public())
	require.NoError(t, err)
	require.NoError(t, pub.Set(jwk.KeyIDKey, testKid))
	require.NoError(t, pub.Set(jwk.AlgorithmKey, jwa.RS256))
	set := jwk.NewSet()
	require.NoError(t, set.AddKey(pub))
	return priv, set
}

func sign(t *testing.T, priv *rsa.PrivateKey, edit func(*jwt.Builder) *jwt.Builder) string {
	t.Helper()
	b := jwt.NewBuilder().
		Issuer(testIssuer).
		Audience([]string{testAudience}).
		Subject(uuid.NewString()).
		Expiration(time.Now().Add(time.Hour))
	if edit != nil {
		b = edit(b)
	}
	tok, err := b.Build()
	require.NoError(t, err)
	hdrs := jws.NewHeaders()
	require.NoError(t, hdrs.Set(jws.KeyIDKey, testKid))
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, priv, jws.WithProtectedHeaders(hdrs)))
	require.NoError(t, err)
	return string(signed)
}

func TestValidateToken_AcceptsValidTokenAndReturnsSub(t *testing.T) {
	priv, set := newKey(t)
	sub := uuid.New()
	v := middleware.NewStaticJWTValidator(set, testIssuer, testAudience)

	got, err := v.ValidateToken(context.Background(), sign(t, priv, func(b *jwt.Builder) *jwt.Builder {
		return b.Subject(sub.String())
	}))

	require.NoError(t, err)
	assert.Equal(t, sub, got.UUID())
}

func TestValidateToken_Rejects(t *testing.T) {
	priv, set := newKey(t)
	otherPriv, _ := newKey(t)

	cases := map[string]string{
		"wrong issuer": sign(t, priv, func(b *jwt.Builder) *jwt.Builder { return b.Issuer("https://evil.example.com") }),
		"wrong audience": sign(t, priv, func(b *jwt.Builder) *jwt.Builder {
			return b.Audience([]string{"anon"})
		}),
		"expired": sign(t, priv, func(b *jwt.Builder) *jwt.Builder {
			return b.Expiration(time.Now().Add(-time.Hour))
		}),
		"not yet valid": sign(t, priv, func(b *jwt.Builder) *jwt.Builder {
			return b.NotBefore(time.Now().Add(time.Hour))
		}),
		"signed by unknown key": sign(t, otherPriv, nil),
		"missing sub":           sign(t, priv, func(b *jwt.Builder) *jwt.Builder { return b.Subject("") }),
		"sub is not a uuid":     sign(t, priv, func(b *jwt.Builder) *jwt.Builder { return b.Subject("not-a-uuid") }),
		"anonymous session":     sign(t, priv, func(b *jwt.Builder) *jwt.Builder { return b.Claim("is_anonymous", true) }),
		"garbage":               "not.a.jwt",
		"empty":                 "",
	}

	v := middleware.NewStaticJWTValidator(set, testIssuer, testAudience)
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := v.ValidateToken(context.Background(), token)
			assert.Error(t, err)
			assert.Equal(t, model.UserId{}, got)
		})
	}
}

func TestValidateToken_RejectsUnsignedToken(t *testing.T) {
	_, set := newKey(t)
	tok, err := jwt.NewBuilder().Issuer(testIssuer).Audience([]string{testAudience}).
		Subject(uuid.NewString()).Expiration(time.Now().Add(time.Hour)).Build()
	require.NoError(t, err)
	unsigned, err := jwt.NewSerializer().Serialize(tok)
	require.NoError(t, err)

	_, err = middleware.NewStaticJWTValidator(set, testIssuer, testAudience).ValidateToken(context.Background(), string(unsigned))
	assert.Error(t, err)
}

func TestValidateToken_MissingSubErrorIsExplicit(t *testing.T) {
	priv, set := newKey(t)
	token := sign(t, priv, func(b *jwt.Builder) *jwt.Builder { return b.Subject("") })

	_, err := middleware.NewStaticJWTValidator(set, testIssuer, testAudience).ValidateToken(context.Background(), token)
	assert.EqualError(t, err, "jwt missing sub claim")
}

func TestValidateToken_NonUUIDSubIsInvalidUUID(t *testing.T) {
	priv, set := newKey(t)
	token := sign(t, priv, func(b *jwt.Builder) *jwt.Builder { return b.Subject("abc") })

	_, err := middleware.NewStaticJWTValidator(set, testIssuer, testAudience).ValidateToken(context.Background(), token)
	assert.ErrorIs(t, err, model.ErrInvalidUUID)
}

func TestDevValidator_StillVerifiesTokensThatAreSent(t *testing.T) {
	priv, set := newKey(t)
	devUser := model.NewUserId(uuid.New())
	v := middleware.DevValidator{UserId: devUser, Tokens: middleware.NewStaticJWTValidator(set, testIssuer, testAudience)}

	sub := uuid.New()
	got, err := v.ValidateToken(context.Background(), sign(t, priv, func(b *jwt.Builder) *jwt.Builder { return b.Subject(sub.String()) }))
	require.NoError(t, err)
	assert.Equal(t, sub, got.UUID(), "a signed-in account is itself, not the dev user")

	for _, token := range []string{"anything", "not.a.jwt"} {
		_, err := v.ValidateToken(context.Background(), token)
		assert.Error(t, err, "a bad token is never upgraded to the dev user")
	}

	// Only a request with no Authorization header at all (the empty token) is the dev user.
	got, err = v.ValidateToken(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, devUser, got)

	_, err = middleware.DevValidator{UserId: devUser}.ValidateToken(context.Background(), "anything")
	assert.Error(t, err, "no token validator fails closed")
}

func TestValidateToken_NoTokenIsRejectedWithoutFetchingKeys(t *testing.T) {
	var hits atomic.Int32
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer jwks.Close()
	v, err := middleware.NewSupabaseJWTValidator(context.Background(), jwks.URL, testIssuer, testAudience)
	require.NoError(t, err)

	_, err = v.ValidateToken(context.Background(), "")
	assert.ErrorIs(t, err, middleware.ErrMissingToken)
	assert.Zero(t, hits.Load(), "an anonymous request must not cost a JWKS fetch (nor turn into a 503 when Supabase is down)")
}

func TestValidateToken_ClientHangingUpIsNotAnAuthOutage(t *testing.T) {
	priv, _ := newKey(t)
	jwksDown := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer jwksDown.Close()
	v, err := middleware.NewSupabaseJWTValidator(context.Background(), jwksDown.URL, testIssuer, testAudience)
	require.NoError(t, err)
	token := sign(t, priv, nil)

	gone, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = v.ValidateToken(gone, token)
	assert.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, middleware.ErrSigningKeysUnavailable)

	// The middleware writes nothing for a client that's gone (no fake 503 in logs and metrics).
	req := httptest.NewRequest("GET", "/api/v1/holdings", nil).WithContext(gone)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	called := false
	middleware.AuthMiddleware(v)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(rec, req)
	assert.False(t, called)
	assert.Zero(t, rec.Body.Len())

	// A live client against an unreachable JWKS is the real outage.
	_, err = v.ValidateToken(context.Background(), token)
	assert.ErrorIs(t, err, middleware.ErrSigningKeysUnavailable)
}

func TestAuthMiddleware_DevModeKeepsAccountsApart(t *testing.T) {
	priv, set := newKey(t)
	devUser := model.NewUserId(uuid.New())
	mw := middleware.AuthMiddleware(middleware.DevValidator{UserId: devUser, Tokens: middleware.NewStaticJWTValidator(set, testIssuer, testAudience)})

	whoAmI := func(header string) (int, model.UserId) {
		var got model.UserId
		req := httptest.NewRequest("GET", "/api/v1/holdings", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got, _ = middleware.GetUserFromContext(r.Context())
		})).ServeHTTP(rec, req)
		return rec.Code, got
	}

	alice, bob := uuid.New(), uuid.New()
	token := func(sub uuid.UUID) string {
		return "Bearer " + sign(t, priv, func(b *jwt.Builder) *jwt.Builder { return b.Subject(sub.String()) })
	}

	code, got := whoAmI("")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, devUser, got)

	code, got = whoAmI(token(alice))
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, alice, got.UUID())

	code, got = whoAmI(token(bob))
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, bob, got.UUID())

	code, _ = whoAmI("Bearer forged")
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestAuthMiddleware_DevValidatorNeedsNoHeader(t *testing.T) {
	user := model.NewUserId(uuid.New())
	var got model.UserId
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = middleware.GetUserFromContext(r.Context())
	})

	rec := httptest.NewRecorder()
	middleware.AuthMiddleware(middleware.DevValidator{UserId: user})(next).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, user, got)
}

func TestAuthMiddleware_RejectsMalformedHeaders(t *testing.T) {
	priv, set := newKey(t)
	valid := sign(t, priv, nil)
	v := middleware.NewStaticJWTValidator(set, testIssuer, testAudience)

	for name, header := range map[string]string{
		"basic scheme":     "Basic " + valid,
		"no scheme":        valid,
		"bearer no token":  "Bearer ",
		"bearer blank":     "Bearer    ",
		"bearer bad token": "Bearer nope",
	} {
		t.Run(name, func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
			req := httptest.NewRequest("GET", "/api/v1/holdings", nil)
			req.Header.Set("Authorization", header)
			rec := httptest.NewRecorder()

			middleware.AuthMiddleware(v)(next).ServeHTTP(rec, req)

			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.False(t, called)
		})
	}
}

func TestAuthMiddleware_BearerSchemeIsCaseInsensitive(t *testing.T) {
	priv, set := newKey(t)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "bearer "+sign(t, priv, nil))
	rec := httptest.NewRecorder()

	middleware.AuthMiddleware(middleware.NewStaticJWTValidator(set, testIssuer, testAudience))(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}
