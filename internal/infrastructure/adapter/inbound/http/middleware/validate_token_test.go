package middleware_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
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

func TestDevValidator_ReturnsFixedUserForAnyToken(t *testing.T) {
	user := model.NewUserId(uuid.New())
	v := middleware.DevValidator{UserId: user}

	for _, token := range []string{"", "anything", "not.a.jwt"} {
		got, err := v.ValidateToken(context.Background(), token)
		require.NoError(t, err)
		assert.Equal(t, user, got)
	}
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
