package http_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createHolding(t *testing.T, router http.Handler, name, platform string, value float64) dto.HoldingResponse {
	t.Helper()
	rec := do(t, router, "POST", "/api/v1/holdings", dto.CreateHoldingRequest{Name: name, AssetClass: "Cash", Platform: platform, ValueUsd: value})
	require.Equal(t, http.StatusCreated, rec.Code)
	var h dto.HoldingResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&h))
	return h
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&v), rec.Body.String())
	return v
}

func TestMovementHandler_RecordsListsAndUndoes(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))
	savings := createHolding(t, r, "Savings", "Santander", 1000)

	rec := do(t, r, "POST", "/api/v1/movements", json.RawMessage(`{"kind": "GAIN", "holdingId": "`+savings.Id+`", "amountUsd": 25.5, "occurredAt": "2026-08-01", "note": " Dividends "}`))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	gain := decode[dto.MovementResponse](t, rec)
	assert.Equal(t, "/api/v1/movements/"+gain.Id, rec.Header().Get("Location"))
	assert.Equal(t, "GAIN", gain.Kind)
	assert.Equal(t, 25.5, gain.AmountUsd)
	assert.Nil(t, gain.FeeUsd, "only transfers have a fee")
	assert.Equal(t, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), gain.OccurredAt, "a date is noon UTC")
	assert.Equal(t, testNow, gain.CreatedAt)
	require.NotNil(t, gain.Holding)
	assert.Equal(t, dto.MovementHoldingResponse{Id: savings.Id, Name: "Savings", Platform: "Santander", AssetClass: "Cash", Exists: true}, *gain.Holding)
	assert.Nil(t, gain.ToHolding)
	assert.Equal(t, "Dividends", *gain.Note)
	assert.True(t, gain.Revertible)

	rec = do(t, r, "POST", "/api/v1/movements", json.RawMessage(`{"kind": "TRANSFER", "fromHoldingId": "`+savings.Id+`", "amountUsd": 100, "feeUsd": 2,
		"occurredAt": "2026-08-01T22:30:00-03:00", "toNewHolding": {"name": "USD cash", "assetClass": "Cash", "platform": "Balanz"}}`))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	transfer := decode[dto.MovementResponse](t, rec)
	assert.Equal(t, 2.0, *transfer.FeeUsd)
	assert.Equal(t, "USD cash", transfer.ToHolding.Name)
	assert.Equal(t, "Balanz", transfer.ToHolding.Platform)
	assert.Equal(t, time.Date(2026, 8, 2, 1, 30, 0, 0, time.UTC), transfer.OccurredAt, "an instant is kept, in UTC")
	assert.Nil(t, transfer.Note)

	rec = do(t, r, "GET", "/api/v1/movements", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	list := decode[dto.MovementListResponse](t, rec)
	require.Len(t, list.Items, 3, "the OPENING, the gain and the transfer (it opened the holding it created)")
	assert.Equal(t, "TRANSFER", list.Items[0].Kind)
	assert.Nil(t, list.NextCursor)

	rec = do(t, r, "DELETE", "/api/v1/movements/"+gain.Id, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "900.00", r.holdings.holdings[savings.Id].Value.String())

	for _, id := range []string{gain.Id, "not-a-uuid"} {
		rec = do(t, r, "DELETE", "/api/v1/movements/"+id, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Contains(t, rec.Body.String(), "Movement "+id+" not found")
	}

	// The OPENING of a holding is undone by removing it.
	opening := list.Items[len(list.Items)-1]
	require.Equal(t, "OPENING", opening.Kind)
	assert.False(t, opening.Revertible)
	rec = do(t, r, "DELETE", "/api/v1/movements/"+opening.Id, nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, middleware.ProblemBaseURI+"/not-revertible", decode[middleware.ProblemDetail](t, rec).Type)
}

func TestMovementHandler_RecordValidation(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))
	savings := createHolding(t, r, "Savings", "Santander", 10)
	problem := func(status int, body string) middleware.ProblemDetail {
		t.Helper()
		rec := do(t, r, "POST", "/api/v1/movements", json.RawMessage(body))
		require.Equal(t, status, rec.Code, rec.Body.String())
		return decode[middleware.ProblemDetail](t, rec)
	}

	assert.Equal(t, "Malformed JSON body", problem(400, `{"amountUsd": "ten"}`).Detail)
	assert.Equal(t, "occurredAt must be a date (YYYY-MM-DD) or a date and time (RFC 3339)",
		problem(400, `{"kind": "GAIN", "holdingId": "`+savings.Id+`", "amountUsd": 1, "occurredAt": "yesterday"}`).Detail)
	assert.Equal(t, "Amount is too large", problem(400, `{"kind": "GAIN", "holdingId": "`+savings.Id+`", "amountUsd": 1e30}`).Detail)
	assert.Equal(t, "Holding nope not found", problem(404, `{"kind": "GAIN", "holdingId": "nope", "amountUsd": 1}`).Detail)
	assert.Equal(t, "Holding nope not found", problem(404, `{"kind": "TRANSFER", "fromHoldingId": "`+savings.Id+`", "toHoldingId": "nope", "amountUsd": 1}`).Detail)
	prob := problem(400, `{"kind": "REFUND", "holdingId": "`+savings.Id+`", "amountUsd": 1}`)
	assert.Equal(t, middleware.ProblemBaseURI+"/validation", prob.Type)
	assert.Equal(t, "kind", prob.Errors[0].Field)
	assert.Equal(t, model.ErrNonPositiveAmount.Error(), problem(400, `{"kind": "GAIN", "holdingId": "`+savings.Id+`", "amountUsd": 0}`).Detail)

	prob = problem(409, `{"kind": "LOSS", "holdingId": "`+savings.Id+`", "amountUsd": 11}`)
	assert.Equal(t, middleware.ProblemBaseURI+"/insufficient-balance", prob.Type)
	assert.Equal(t, "Savings is worth $10.00: a loss can't be larger than that.", prob.Detail)
}

func TestMovementHandler_ListQuery(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))
	savings := createHolding(t, r, "Savings", "Santander", 10)
	createHolding(t, r, "Cash", "Balanz", 10)
	createHolding(t, r, "More", "Balanz", 10)

	for query, field := range map[string]string{
		"holdingId=nope":    "holdingId",
		"kind=GAIN,REFUND":  "kind",
		"from=last-week":    "from",
		"to=2026-13-01":     "to",
		"limit=0":           "limit",
		"limit=201":         "limit",
		"limit=ten":         "limit",
		"cursor=not*base64": "cursor",
		"cursor=" + base64.RawURLEncoding.EncodeToString([]byte("a|b")):                   "cursor",
		"cursor=" + base64.RawURLEncoding.EncodeToString([]byte("a|b|"+uuid.NewString())): "cursor",
	} {
		rec := do(t, r, "GET", "/api/v1/movements?"+query, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code, query)
		assert.Equal(t, field, decode[middleware.ProblemDetail](t, rec).Errors[0].Field, query)
	}

	rec := do(t, r, "GET", "/api/v1/movements?holdingId="+savings.Id+"&kind=GAIN,%20LOSS&from=2026-08-01&to=2026-08-31", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	f := r.movements.lastFilter
	assert.Equal(t, savings.Id, f.HoldingId.String())
	assert.Equal(t, []model.MovementKind{model.MovementGain, model.MovementLoss}, f.Kinds)
	assert.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), *f.From, "a date's first instant")
	assert.Equal(t, time.Date(2026, 8, 31, 23, 59, 59, 999999999, time.UTC), *f.To, "a date's last instant")
	assert.Equal(t, 50, r.movements.lastLimit)

	// A page at a time: the cursor goes back as it came.
	rec = do(t, r, "GET", "/api/v1/movements?limit=2", nil)
	page := decode[dto.MovementListResponse](t, rec)
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.NextCursor)
	rec = do(t, r, "GET", "/api/v1/movements?limit=2&cursor="+*page.NextCursor, nil)
	rest := decode[dto.MovementListResponse](t, rec)
	require.Len(t, rest.Items, 1)
	assert.Equal(t, page.Items[1].Id, r.movements.lastAfter.Id.String())
	assert.True(t, r.movements.lastAfter.OccurredAt.Equal(page.Items[1].OccurredAt))
	assert.Nil(t, rest.NextCursor)
}

func TestHoldingHandler_UpdateRecordsWhyTheValueChanged(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))
	savings := createHolding(t, r, "Savings", "Santander", 100)

	rec := do(t, r, "PATCH", "/api/v1/holdings/"+savings.Id, json.RawMessage(`{"valueUsd": 150, "valueChangeReason": "CASH_FLOW", "occurredAt": "2026-08-29", "note": "salary"}`))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	m := r.movements.movements[len(r.movements.movements)-1]
	assert.Equal(t, model.MovementDeposit, m.Kind)
	assert.Equal(t, "50.00", m.Amount.String())
	assert.Equal(t, time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC), m.OccurredAt)
	assert.Equal(t, "salary", m.Note)

	for body, detail := range map[string]string{
		`{"valueUsd": 1, "occurredAt": "soon"}`:         "occurredAt must be a date (YYYY-MM-DD) or a date and time (RFC 3339)",
		`{"valueUsd": 1, "valueChangeReason": "TAXES"}`: `valueChangeReason must be one of MARKET, CASH_FLOW, CORRECTION (got "TAXES")`,
	} {
		rec := do(t, r, "PATCH", "/api/v1/holdings/"+savings.Id, json.RawMessage(body))
		require.Equal(t, http.StatusBadRequest, rec.Code, body)
		assert.Equal(t, detail, decode[middleware.ProblemDetail](t, rec).Detail)
	}
}
