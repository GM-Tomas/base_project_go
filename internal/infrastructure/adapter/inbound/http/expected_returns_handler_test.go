package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHoldingHandler_ExpectedReturnOnCreateAndPatch(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))

	rec := do(t, r, "POST", "/api/v1/holdings", json.RawMessage(`{"name": "VOO", "assetClass": "Index Fund", "platform": "IBKR",
		"valueUsd": 1000, "expectedReturnPct": 7.456}`))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	voo := decode[dto.HoldingResponse](t, rec)
	assert.Equal(t, 7.46, *voo.ExpectedReturnPct)
	assert.Equal(t, 7.46, *voo.EffectiveReturnPct)

	cash := createHolding(t, r, "Cash", "Bank", 50)
	assert.Nil(t, cash.ExpectedReturnPct)
	assert.Nil(t, cash.EffectiveReturnPct)
	// The key is always there, null when not set.
	rec = do(t, r, "GET", "/api/v1/holdings", nil)
	assert.Contains(t, rec.Body.String(), `"expectedReturnPct":null`)

	rec = do(t, r, "PATCH", "/api/v1/holdings/"+voo.Id, json.RawMessage(`{"expectedReturnPct": -2.5}`))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, -2.5, *decode[dto.HoldingResponse](t, rec).ExpectedReturnPct)
	rec = do(t, r, "PATCH", "/api/v1/holdings/"+voo.Id, json.RawMessage(`{"expectedReturnPct": null}`))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, decode[dto.HoldingResponse](t, rec).ExpectedReturnPct)

	for method, path := range map[string]string{"POST": "/api/v1/holdings", "PATCH": "/api/v1/holdings/" + voo.Id} {
		rec = do(t, r, method, path, json.RawMessage(`{"name": "x", "assetClass": "Cash", "platform": "Bank", "valueUsd": 1,
			"expectedReturnPct": 100.01}`))
		require.Equal(t, http.StatusBadRequest, rec.Code, method)
		prob := decode[middleware.ProblemDetail](t, rec)
		assert.Equal(t, "expectedReturnPct must be between -100 and 100", prob.Detail, method)
		assert.Equal(t, "expectedReturnPct", prob.Errors[0].Field)
	}
}

func TestHoldingHandler_SetsExpectedReturnsAtOnce(t *testing.T) {
	user := model.NewUserId(uuid.New())
	r := newTestRouter(user)
	voo := createHolding(t, r, "VOO", "IBKR", 6000)
	bond := createHolding(t, r, "Bond", "Balanz", 3000)
	put := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		return do(t, r, "PUT", "/api/v1/holdings/expected-returns", json.RawMessage(body))
	}

	rec := put(`{"items": [{"holdingId": "` + voo.Id + `", "expectedReturnPct": 8}, {"holdingId": "` + bond.Id + `", "expectedReturnPct": 4.5}]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	list := decode[[]dto.HoldingResponse](t, rec)
	require.Len(t, list, 2)
	assert.Equal(t, voo.Id, list[0].Id, "in the order asked")
	assert.Equal(t, 8.0, *list[0].ExpectedReturnPct)
	assert.Equal(t, 4.5, *list[1].ExpectedReturnPct)

	rec = put(`{"items": [{"holdingId": "` + bond.Id + `", "expectedReturnPct": null}]}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, decode[[]dto.HoldingResponse](t, rec)[0].ExpectedReturnPct)
	assert.JSONEq(t, "[]", put(`{"items": []}`).Body.String())

	// Every problem with the items at once.
	rec = put(`{"items": [{"holdingId": "", "expectedReturnPct": 1}, {"holdingId": "` + voo.Id + `"},
		{"holdingId": "` + voo.Id + `", "expectedReturnPct": 101}]}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	prob := decode[middleware.ProblemDetail](t, rec)
	assert.Equal(t, "items[0].holdingId is required; items[1].expectedReturnPct is required (null clears it); "+
		"items[2].holdingId appears more than once; items[2].expectedReturnPct must be between -100 and 100", prob.Detail)
	assert.Equal(t, "items[0].holdingId", prob.Errors[0].Field)

	assert.Equal(t, "items is required", decode[middleware.ProblemDetail](t, put(`{}`)).Detail)
	assert.Equal(t, "Malformed JSON body", decode[middleware.ProblemDetail](t, put(`{"items": {}}`)).Detail)
	many := strings.Repeat(`{"holdingId": "x", "expectedReturnPct": 1},`, model.MaxHoldingsPerUser+1)
	assert.Equal(t, "items can't have more than 1000 entries",
		decode[middleware.ProblemDetail](t, put(`{"items": [`+strings.TrimSuffix(many, ",")+`]}`)).Detail)

	// An id that isn't a holding's, or someone else's: 404, nothing changed.
	for _, id := range []string{"nope", uuid.NewString()} {
		rec = put(`{"items": [{"holdingId": "` + voo.Id + `", "expectedReturnPct": 1}, {"holdingId": "` + id + `", "expectedReturnPct": 1}]}`)
		require.Equal(t, http.StatusNotFound, rec.Code, id)
		assert.Equal(t, "Holding "+id+" not found", decode[middleware.ProblemDetail](t, rec).Detail)
	}
	assert.Equal(t, "8", r.holdings.holdings[voo.Id].ExpectedReturnPct.String())
}
