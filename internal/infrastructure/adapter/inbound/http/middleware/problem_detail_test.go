package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func handle(t *testing.T, err error) (*httptest.ResponseRecorder, middleware.ProblemDetail) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/holdings", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.TraceIDContextKey, "trace-123"))
	rec := httptest.NewRecorder()

	middleware.HandleError(rec, req, err)

	var prob middleware.ProblemDetail
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prob))
	}
	return rec, prob
}

func TestHandleError_NilWritesNothing(t *testing.T) {
	rec, _ := handle(t, nil)
	assert.Equal(t, 0, rec.Body.Len())
	assert.Empty(t, rec.Header().Get("Content-Type"))
}

func TestHandleError_MapsErrorsToStatus(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		slug   string
		detail string
	}{
		{"not found", appErrors.NewResourceNotFoundError("Holding x not found"), http.StatusNotFound, "not-found", "Holding x not found"},
		{"duplicate", appErrors.NewDuplicateResourceError("Snapshot exists"), http.StatusConflict, "conflict", "Snapshot exists"},
		{"in use", appErrors.NewResourceInUseError("Platform in use"), http.StatusConflict, "conflict", "Platform in use"},
		{"blank label", fmt.Errorf("Holding name %w", model.ErrBlankLabel), http.StatusBadRequest, "bad-request", "Holding name must not be blank"},
		{"label too long", fmt.Errorf("x %w", model.ErrLabelTooLong), http.StatusBadRequest, "bad-request", "x exceeds max length"},
		{"negative money", model.ErrNegativeMoney, http.StatusBadRequest, "bad-request", model.ErrNegativeMoney.Error()},
		{"non-finite money", model.ErrNonFiniteMoney, http.StatusBadRequest, "bad-request", model.ErrNonFiniteMoney.Error()},
		{"invalid uuid", model.ErrInvalidUUID, http.StatusBadRequest, "bad-request", model.ErrInvalidUUID.Error()},
		{"years out of range", model.ErrYearsOutOfRange, http.StatusBadRequest, "bad-request", model.ErrYearsOutOfRange.Error()},
		{"yield out of range", model.ErrYieldOutOfRange, http.StatusBadRequest, "bad-request", model.ErrYieldOutOfRange.Error()},
		{"too many milestones", model.ErrTooManyMilestones, http.StatusBadRequest, "bad-request", model.ErrTooManyMilestones.Error()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, prob := handle(t, tc.err)

			assert.Equal(t, tc.status, rec.Code)
			assert.Equal(t, middleware.ProblemJSONHeader, rec.Header().Get("Content-Type"))
			assert.Equal(t, middleware.ProblemBaseURI+"/"+tc.slug, prob.Type)
			assert.Equal(t, tc.status, prob.Status)
			assert.Equal(t, tc.detail, prob.Detail)
			assert.Equal(t, "/api/v1/holdings", prob.Instance)
			assert.Equal(t, "trace-123", prob.TraceID)
			assert.Empty(t, prob.Errors)
		})
	}
}

func TestHandleError_ValidationErrorsListEveryField(t *testing.T) {
	rec, prob := handle(t, appErrors.NewValidationErrors([]appErrors.ValidationError{
		{Field: "name", Message: "name is required"},
		{Field: "valueUsd", Message: "valueUsd must be >= 0"},
	}))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, middleware.ProblemBaseURI+"/validation", prob.Type)
	assert.Equal(t, "name is required; valueUsd must be >= 0", prob.Detail)
	assert.Equal(t, []middleware.FieldError{
		{Field: "name", Message: "name is required"},
		{Field: "valueUsd", Message: "valueUsd must be >= 0"},
	}, prob.Errors)
}

func TestHandleError_UnknownErrorIs500AndDoesNotLeak(t *testing.T) {
	rec, prob := handle(t, errors.New("mongo: connection refused at 10.0.0.5:27017"))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, middleware.ProblemBaseURI+"/internal", prob.Type)
	assert.NotContains(t, rec.Body.String(), "10.0.0.5")
	assert.Equal(t, "trace-123", prob.TraceID)
}

func TestWriteUnauthorized_DefaultsDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	middleware.WriteUnauthorized(rec, httptest.NewRequest("GET", "/x", nil), "")

	var prob middleware.ProblemDetail
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prob))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "Missing or invalid access token", prob.Detail)
	assert.Empty(t, prob.TraceID)
}

func TestWriteProblem_DefaultsTitleToStatusText(t *testing.T) {
	rec := httptest.NewRecorder()
	middleware.WriteProblem(rec, httptest.NewRequest("GET", "/x", nil), http.StatusTeapot, "tea", "", "d", nil)

	var prob middleware.ProblemDetail
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prob))
	assert.Equal(t, http.StatusText(http.StatusTeapot), prob.Title)
}
