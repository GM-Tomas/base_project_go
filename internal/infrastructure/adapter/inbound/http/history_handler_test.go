package http_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// F6: what a period's movements add up to, and checkpoints from the past.

func TestMovementHandler_SummarizesAPeriod(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))
	savings := createHolding(t, r, "Savings", "Bank", 1000)
	fund := createHolding(t, r, "Fund", "Broker", 500)
	for _, body := range []string{
		`{"kind": "GAIN", "holdingId": "` + fund.Id + `", "amountUsd": 60, "occurredAt": "2026-08-10"}`,
		`{"kind": "DEPOSIT", "holdingId": "` + savings.Id + `", "amountUsd": 400, "occurredAt": "2026-08-20"}`,
		`{"kind": "TRANSFER", "fromHoldingId": "` + savings.Id + `", "toHoldingId": "` + fund.Id + `", "amountUsd": 200, "feeUsd": 2, "occurredAt": "2026-07-01"}`,
	} {
		require.Equal(t, http.StatusCreated, do(t, r, "POST", "/api/v1/movements", json.RawMessage(body)).Code, body)
	}

	rec := do(t, r, "GET", "/api/v1/movements/summary", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	all := decode[dto.MovementsSummaryResponse](t, rec)
	assert.Equal(t, time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), all.From)
	assert.Equal(t, testNow, all.To)
	assert.Equal(t, 5, all.Count) // 2 openings and 3 recorded
	assert.Equal(t, 1, all.Transfers)
	assert.Len(t, all.TotalsUsd, len(model.SummaryBuckets))
	assert.Equal(t, 1500.0, all.TotalsUsd["OPENING"])
	assert.Equal(t, 2.0, all.TotalsUsd["TRANSFER_FEES"])
	assert.Equal(t, dto.NetWorthEffectDTO{Investments: 58, Saving: 400, AddedRemoved: 1500, Corrections: 0}, all.NetWorthEffectUsd)

	// August only: dates cover their whole UTC day.
	rec = do(t, r, "GET", "/api/v1/movements/summary?from=2026-08-01&to=2026-08-20", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	august := decode[dto.MovementsSummaryResponse](t, rec)
	assert.Equal(t, time.Date(2026, 8, 20, 23, 59, 59, 999999999, time.UTC), august.To)
	assert.Equal(t, 2, august.Count)
	assert.Equal(t, dto.NetWorthEffectDTO{Investments: 60, Saving: 400}, august.NetWorthEffectUsd)
	assert.Contains(t, do(t, r, "GET", "/api/v1/movements/summary?from=2026-08-01", nil).Body.String(), `"netWorthEffectUsd":{"investments":60,"saving":400,"addedRemoved":1500,"corrections":0}`, "until now: the holdings were added today")

	rec = do(t, r, "GET", "/api/v1/movements/summary?from=yesterday&to=2026-13-01", nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	p := decode[middleware.ProblemDetail](t, rec)
	assert.Len(t, p.Errors, 2)
	rec = do(t, r, "GET", "/api/v1/movements/summary?from=2026-09-01&to=2026-08-01", nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "from must not be after to", decode[middleware.ProblemDetail](t, rec).Detail)
}

func TestWealthHandler_KeepsAPastCheckpoint(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))

	rec := do(t, r, "POST", "/api/v1/wealth/snapshots", json.RawMessage(`{"capturedAt": "2025-12-31", "totalValueUsd": 81000,
		"assetsUsd": 95000, "debtsUsd": 14000, "note": " From my spreadsheet "}`))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	past := decode[dto.SnapshotResponse](t, rec)
	assert.Equal(t, time.Date(2025, 12, 31, 12, 0, 0, 0, time.UTC), past.CapturedAt, "a date is noon UTC")
	assert.Equal(t, "MANUAL", past.Source)
	assert.Equal(t, "From my spreadsheet", *past.Note)
	assert.Equal(t, 95000.0, past.AssetsUsd)
	assert.Equal(t, 14000.0, past.DebtsUsd)

	// No body, or {}: today's, as always.
	rec = do(t, r, "POST", "/api/v1/wealth/snapshots", nil)
	require.Equal(t, http.StatusCreated, rec.Code)
	today := decode[dto.SnapshotResponse](t, rec)
	assert.Equal(t, "AUTO", today.Source)
	assert.Nil(t, today.Note)
	assert.Contains(t, do(t, r, "GET", "/api/v1/wealth/snapshots", nil).Body.String(), `"source":"MANUAL","note":"From my spreadsheet"`)
	assert.Equal(t, http.StatusConflict, do(t, r, "POST", "/api/v1/wealth/snapshots", json.RawMessage(`{}`)).Code, "today's again, in the same second")

	for _, tc := range []struct {
		body   string
		fields []string
		detail string
	}{
		{`{"totalValueUsd": 5}`, []string{"capturedAt"}, ""},
		{`{"capturedAt": "2025-01-01"}`, []string{"totalValueUsd"}, ""},
		{`{"capturedAt": "soon", "totalValueUsd": 1e16, "assetsUsd": -1, "debtsUsd": 1e16}`, []string{"capturedAt", "totalValueUsd", "assetsUsd", "debtsUsd"}, ""},
		{`{"capturedAt": "2025-01-01", "totalValueUsd": 5, "assetsUsd": 5}`, nil, "assetsUsd and debtsUsd go together: send both or neither"},
		{`{"capturedAt": "2025-01-01", "totalValueUsd": 5, "assetsUsd": 9, "debtsUsd": 1}`, nil, "totalValueUsd must be assetsUsd − debtsUsd"},
		{`{"capturedAt": "2027-01-01", "totalValueUsd": 5}`, nil, "capturedAt must be in the past, from 1970 on"},
	} {
		rec := do(t, r, "POST", "/api/v1/wealth/snapshots", json.RawMessage(tc.body))
		require.Equal(t, http.StatusBadRequest, rec.Code, tc.body)
		p := decode[middleware.ProblemDetail](t, rec)
		if tc.detail != "" {
			assert.Equal(t, tc.detail, p.Detail, tc.body)
			continue
		}
		fields := make([]string, len(p.Errors))
		for i, e := range p.Errors {
			fields[i] = e.Field
		}
		assert.Equal(t, tc.fields, fields, tc.body)
	}
	assert.Equal(t, http.StatusBadRequest, doRaw(r, "POST", "/api/v1/wealth/snapshots", `{"capturedAt":`).Code)

	rec = do(t, r, "POST", "/api/v1/wealth/snapshots", json.RawMessage(`{"capturedAt": "2025-12-31T12:00:00Z", "totalValueUsd": 1}`))
	require.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "A snapshot already exists for 2025-12-31T12:00:00Z", decode[middleware.ProblemDetail](t, rec).Detail)
}
