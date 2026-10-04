package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/infrastructure/app"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/config"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
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
