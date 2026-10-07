package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/infrastructure/app"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/config"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each test gets its own database (see newE2E), so they run in parallel.
func testConfig(t *testing.T) config.Config {
	t.Helper()
	t.Parallel()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("MONGO_TEST_URI not set (make test-coverage starts a throwaway Mongo)")
	}
	cfg := config.LoadConfig()
	cfg.MongoDBURI = uri
	cfg.MongoDBName = "test_app"
	cfg.DevUserID = ""
	return cfg
}

func TestBuildApp_FailsWhenMongoIsUnreachable(t *testing.T) {
	cfg := config.LoadConfig()
	cfg.MongoDBURI = "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=100"

	_, err := app.BuildApp(context.Background(), cfg)

	assert.ErrorContains(t, err, "connecting to MongoDB")
}

func TestBuildApp_PreviewNeverConnectsToTheDatabase(t *testing.T) {
	cfg := config.LoadConfig()
	cfg.Preview = true
	cfg.HideDocs = false
	cfg.MongoDBURI = "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=100" // would fail if it tried

	a, err := app.BuildApp(context.Background(), cfg)
	require.NoError(t, err)
	defer a.Cleanup()

	for _, path := range []string{"/api/v1/holdings", "/api/v1/wealth/summary", "/api/v1/anything"} {
		rec := httptest.NewRecorder()
		a.Handler.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, path)
		assert.Contains(t, rec.Body.String(), "preview-without-data", path)
	}
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/health", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	// A preview's router has none of the database's handlers, so it serves only these. A route added
	// outside RouterParams.NoData's branch is served on previews too: make sure it can be, then list it.
	var routes []string
	err = chi.Walk(a.Handler.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		rec := httptest.NewRecorder()
		a.Handler.ServeHTTP(rec, httptest.NewRequest(method, strings.ReplaceAll(route, "*", "x"), nil))
		assert.NotEqual(t, http.StatusInternalServerError, rec.Code, method+" "+route)
		if !slices.Contains(routes, route) {
			routes = append(routes, route)
		}
		return nil
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"/api/v1/health", "/api/v1/*", "/api/v1/openapi.json",
		"/docs", "/docs/*", "/docs/openapi.json", "/openapi.json", "/swagger", "/swagger/*",
	}, routes)
}

func TestBuildApp_DevUserServesProtectedRoutesWithoutToken(t *testing.T) {
	cfg := testConfig(t)
	cfg.DevUserID = uuid.NewString()

	a, err := app.BuildApp(context.Background(), cfg)
	require.NoError(t, err)
	defer a.Cleanup()

	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/holdings", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestBuildApp_RejectsInvalidDevUser(t *testing.T) {
	cfg := testConfig(t)
	cfg.DevUserID = "not-a-uuid"

	_, err := app.BuildApp(context.Background(), cfg)

	assert.ErrorContains(t, err, "AUTH_DEV_USER_ID")
}

func TestBuildApp_RequiresTokenByDefault(t *testing.T) {
	a, err := app.BuildApp(context.Background(), testConfig(t))
	require.NoError(t, err)
	defer a.Cleanup()

	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/holdings", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/health", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}
