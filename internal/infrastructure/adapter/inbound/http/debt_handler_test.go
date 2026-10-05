package http_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createDebt(t *testing.T, r *testRouter, body string) dto.DebtResponse {
	t.Helper()
	rec := do(t, r, "POST", "/api/v1/debts", json.RawMessage(body))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return decode[dto.DebtResponse](t, rec)
}

func TestDebtHandler_CreatesListsEditsAndRemoves(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))

	rec := do(t, r, "POST", "/api/v1/debts", json.RawMessage(`{"name": " Visa ", "lender": "Banco Galicia", "kind": "CREDIT_CARD",
		"balanceUsd": 1250.4, "interestRatePct": 65, "monthlyPaymentUsd": 300, "dueDay": 10}`))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	visa := decode[dto.DebtResponse](t, rec)
	assert.Equal(t, "/api/v1/debts/"+visa.Id, rec.Header().Get("Location"))
	assert.Equal(t, "Visa", visa.Name)
	assert.Equal(t, "Banco Galicia", *visa.Lender)
	assert.Equal(t, "CREDIT_CARD", visa.Kind)
	assert.Equal(t, 1250.4, visa.BalanceUsd)
	assert.Equal(t, 65.0, *visa.InterestRatePct)
	assert.Equal(t, 300.0, *visa.MonthlyPaymentUsd)
	assert.Equal(t, 10, *visa.DueDay)
	assert.Nil(t, visa.Notes)
	assert.Equal(t, testNow, visa.CreatedAt)
	assert.Equal(t, "ON_TRACK", visa.Payoff.Status)
	assert.Equal(t, 5, *visa.Payoff.Months)
	assert.Equal(t, "2027-01", *visa.Payoff.PayoffMonth)
	assert.Positive(t, *visa.Payoff.TotalInterestUsd)

	mom := createDebt(t, r, `{"name": "Mom", "balanceUsd": 5000}`)
	assert.Equal(t, "OTHER", mom.Kind)
	assert.Nil(t, mom.Lender)
	assert.Nil(t, mom.InterestRatePct)
	assert.Nil(t, mom.MonthlyPaymentUsd)
	assert.Nil(t, mom.DueDay)
	assert.Equal(t, dto.DebtPayoffResponse{Status: "NO_PAYMENT"}, mom.Payoff)

	rec = do(t, r, "GET", "/api/v1/debts", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	list := decode[[]dto.DebtResponse](t, rec)
	require.Len(t, list, 2)
	assert.Equal(t, "Mom", list[0].Name, "largest balance first")

	// A merge patch: null clears what's optional; a new balance says what it was.
	rec = do(t, r, "PATCH", "/api/v1/debts/"+visa.Id, json.RawMessage(`{"lender": null, "interestRatePct": null, "dueDay": 15,
		"notes": "0% until March", "balanceUsd": 950.4, "balanceChangeReason": "PAYMENT", "occurredAt": "2026-08-10", "note": "August"}`))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	edited := decode[dto.DebtResponse](t, rec)
	assert.Nil(t, edited.Lender)
	assert.Nil(t, edited.InterestRatePct)
	assert.Equal(t, 15, *edited.DueDay)
	assert.Equal(t, "0% until March", *edited.Notes)
	assert.Equal(t, 950.4, edited.BalanceUsd)
	assert.Equal(t, 300.0, *edited.MonthlyPaymentUsd, "not sent: as it was")
	assert.Equal(t, 4, *edited.Payoff.Months)
	payment := r.movements.movements[len(r.movements.movements)-1]
	assert.Equal(t, model.MovementDebtPayment, payment.Kind)
	assert.Equal(t, "300.00", payment.Amount.String())
	assert.Equal(t, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC), payment.OccurredAt)
	assert.Equal(t, "August", payment.Note)

	rec = do(t, r, "DELETE", "/api/v1/debts/"+visa.Id, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, model.MovementClosing, r.movements.movements[len(r.movements.movements)-1].Kind)
	for _, id := range []string{visa.Id, "nope"} {
		for _, method := range []string{"PATCH", "DELETE"} {
			rec = do(t, r, method, "/api/v1/debts/"+id, json.RawMessage(`{"name": "x"}`))
			assert.Equal(t, http.StatusNotFound, rec.Code, method)
			assert.Equal(t, "Debt "+id+" not found", decode[middleware.ProblemDetail](t, rec).Detail)
		}
	}
}

func TestDebtHandler_Validation(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))
	problem := func(method, path, body string) middleware.ProblemDetail {
		t.Helper()
		rec := do(t, r, method, path, json.RawMessage(body))
		require.Equal(t, http.StatusBadRequest, rec.Code, body)
		return decode[middleware.ProblemDetail](t, rec)
	}

	assert.Equal(t, "Malformed JSON body", problem("POST", "/api/v1/debts", `{"balanceUsd": "lots"}`).Detail)
	prob := problem("POST", "/api/v1/debts", `{"name": " ", "kind": "CARD", "interestRatePct": 201, "monthlyPaymentUsd": -1, "dueDay": 0}`)
	assert.Equal(t, "Name is required; kind must be one of CREDIT_CARD, LOAN, MORTGAGE, PERSONAL, OTHER; Balance is required; "+
		"interestRatePct must be between 0 and 200; Monthly payment must not be negative; dueDay must be between 1 and 31", prob.Detail)
	var fields []string
	for _, e := range prob.Errors {
		fields = append(fields, e.Field)
	}
	assert.Equal(t, []string{"name", "kind", "balanceUsd", "interestRatePct", "monthlyPaymentUsd", "dueDay"}, fields)
	assert.Equal(t, "Balance must not be negative", problem("POST", "/api/v1/debts", `{"name": "x", "balanceUsd": -1}`).Detail)
	assert.Equal(t, "Balance is too large", problem("POST", "/api/v1/debts", `{"name": "x", "balanceUsd": 1e16}`).Detail)
	assert.Equal(t, "Monthly payment is too large", problem("POST", "/api/v1/debts", `{"name": "x", "balanceUsd": 1, "monthlyPaymentUsd": 1e16}`).Detail)
	assert.Equal(t, "Debt name exceeds max length (121 > 120)",
		problem("POST", "/api/v1/debts", `{"name": "`+longName+`", "balanceUsd": 1}`).Detail)

	visa := createDebt(t, r, `{"name": "Visa", "balanceUsd": 100}`)
	path := "/api/v1/debts/" + visa.Id
	assert.Equal(t, "Malformed JSON body", problem("PATCH", path, `{"dueDay": 1.5}`).Detail)
	prob = problem("PATCH", path, `{"name": null, "kind": null, "balanceUsd": null, "interestRatePct": -1, "monthlyPaymentUsd": 1e16,
		"dueDay": 32, "occurredAt": "soon"}`)
	assert.Equal(t, "Name is required; Kind is required; Balance is required; interestRatePct must be between 0 and 200; "+
		"Monthly payment is too large; dueDay must be between 1 and 31; occurredAt must be a date (YYYY-MM-DD) or a date and time (RFC 3339)", prob.Detail)
	assert.Equal(t, kindProblem, problem("PATCH", path, `{"kind": "card"}`).Detail)
	assert.Equal(t, "Balance must not be negative", problem("PATCH", path, `{"balanceUsd": -5}`).Detail)
	assert.Equal(t, `balanceChangeReason must be one of PAYMENT, CHARGE, INTEREST, CORRECTION (got "GIFT")`,
		problem("PATCH", path, `{"balanceUsd": 5, "balanceChangeReason": "GIFT"}`).Detail)
	assert.Equal(t, "A payment can only lower the balance", problem("PATCH", path, `{"balanceUsd": 500, "balanceChangeReason": "PAYMENT"}`).Detail)
	assert.Equal(t, "New charges and interest can only raise the balance",
		problem("PATCH", path, `{"balanceUsd": 5, "balanceChangeReason": "CHARGE"}`).Detail)
	assert.Equal(t, 100.0, r.debts.debts[visa.Id].Balance.Float64(), "nothing changed")
}

const kindProblem = "kind must be one of CREDIT_CARD, LOAN, MORTGAGE, PERSONAL, OTHER"

var longName = strings.Repeat("x", 121)

func TestDebtHandler_CapsDebtsPerUser(t *testing.T) {
	user := model.NewUserId(uuid.New())
	r := newTestRouter(user)
	for i := 0; i < model.MaxDebtsPerUser; i++ {
		r.debts.debts[uuid.NewString()] = model.Debt{UserId: user, Balance: model.ZeroMoney}
	}
	rec := do(t, r, "POST", "/api/v1/debts", json.RawMessage(`{"name": "one more", "balanceUsd": 1}`))
	assert.Equal(t, http.StatusConflict, rec.Code)
	prob := decode[middleware.ProblemDetail](t, rec)
	assert.Equal(t, middleware.ProblemBaseURI+"/limit-exceeded", prob.Type)
	assert.Equal(t, "You can track up to 200 debts. Remove one to add another.", prob.Detail)
}

func TestMovementHandler_DebtMovements(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))
	savings := createHolding(t, r, "Savings", "Santander", 1000)
	visa := createDebt(t, r, `{"name": "Visa", "lender": "Galicia", "balanceUsd": 1250}`)

	rec := do(t, r, "POST", "/api/v1/movements", json.RawMessage(`{"kind": "DEBT_PAYMENT", "debtId": "`+visa.Id+`", "fromHoldingId": "`+savings.Id+`", "amountUsd": 300}`))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	payment := decode[dto.MovementResponse](t, rec)
	assert.Equal(t, "DEBT_PAYMENT", payment.Kind)
	require.NotNil(t, payment.Debt)
	assert.Equal(t, dto.MovementDebtResponse{Id: visa.Id, Name: "Visa", Lender: ptrTo("Galicia"), Exists: true}, *payment.Debt)
	assert.Equal(t, savings.Id, payment.Holding.Id)
	assert.Nil(t, payment.FeeUsd)
	assert.Equal(t, 950.0, r.debts.debts[visa.Id].Balance.Float64())
	assert.Equal(t, 700.0, r.holdings.holdings[savings.Id].Value.Float64())

	rec = do(t, r, "POST", "/api/v1/movements", json.RawMessage(`{"kind": "DEBT_CHARGE", "debtId": "`+visa.Id+`", "toHoldingId": "`+savings.Id+`", "amountUsd": 50}`))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, savings.Id, decode[dto.MovementResponse](t, rec).ToHolding.Id)

	rec = do(t, r, "POST", "/api/v1/movements", json.RawMessage(`{"kind": "DEBT_PAYMENT", "debtId": "`+visa.Id+`", "amountUsd": 5000}`))
	require.Equal(t, http.StatusConflict, rec.Code)
	prob := decode[middleware.ProblemDetail](t, rec)
	assert.Equal(t, middleware.ProblemBaseURI+"/insufficient-balance", prob.Type)
	assert.Equal(t, "Visa only has $1,000.00 left to pay.", prob.Detail)

	rec = do(t, r, "POST", "/api/v1/movements", json.RawMessage(`{"kind": "DEBT_INTEREST", "amountUsd": 1}`))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "debtId is required", decode[middleware.ProblemDetail](t, rec).Detail)
	rec = do(t, r, "POST", "/api/v1/movements", json.RawMessage(`{"kind": "DEBT_INTEREST", "debtId": "nope", "amountUsd": 1}`))
	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "Debt nope not found", decode[middleware.ProblemDetail](t, rec).Detail)
	rec = do(t, r, "POST", "/api/v1/movements", json.RawMessage(`{"kind": "REFUND", "amountUsd": 1}`))
	assert.Equal(t, "kind must be one of GAIN, LOSS, DEPOSIT, WITHDRAWAL, TRANSFER, DEBT_PAYMENT, DEBT_CHARGE, DEBT_INTEREST",
		decode[middleware.ProblemDetail](t, rec).Detail)

	// One debt's activity.
	rec = do(t, r, "GET", "/api/v1/movements?debtId="+visa.Id+"&kind=DEBT_PAYMENT,DEBT_CHARGE", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, visa.Id, r.movements.lastFilter.DebtId.String())
	assert.Equal(t, []model.MovementKind{model.MovementDebtPayment, model.MovementDebtCharge}, r.movements.lastFilter.Kinds)
	rec = do(t, r, "GET", "/api/v1/movements?debtId=nope", nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "debtId must be a debt's id", decode[middleware.ProblemDetail](t, rec).Detail)

	// Undone: the debt and the holding as they were.
	rec = do(t, r, "DELETE", "/api/v1/movements/"+payment.Id, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, 1300.0, r.debts.debts[visa.Id].Balance.Float64())
	assert.Equal(t, 1050.0, r.holdings.holdings[savings.Id].Value.Float64())

	// Once the debt is gone its movements say so, and can't be undone.
	require.Equal(t, http.StatusNoContent, do(t, r, "DELETE", "/api/v1/debts/"+visa.Id, nil).Code)
	rec = do(t, r, "GET", "/api/v1/movements", nil)
	for _, m := range decode[dto.MovementListResponse](t, rec).Items {
		if m.Debt != nil {
			assert.False(t, m.Debt.Exists, m.Kind)
			assert.False(t, m.Revertible, m.Kind)
		}
	}
}

func ptrTo[T any](v T) *T { return &v }

func TestWealthHandler_NetWorthWithDebts(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))
	r.wealthAgg.assets = model.MustMoneyFromFloat(3000)
	r.wealthAgg.debts = model.DebtTotals{Balance: model.MustMoneyFromFloat(4500.5), Count: 2, MonthlyPayment: model.MustMoneyFromFloat(450)}

	summary := decode[dto.WealthSummaryResponse](t, do(t, r, "GET", "/api/v1/wealth/summary", nil))
	assert.Equal(t, -1500.5, summary.NetWorth.Usd)
	assert.Equal(t, dto.AssetsDTO{Usd: 3000}, summary.Assets)
	assert.Equal(t, dto.DebtsDTO{Usd: 4500.5, Count: 2, MonthlyPaymentUsd: 450}, summary.Debts)

	rec := do(t, r, "POST", "/api/v1/wealth/snapshots", nil)
	require.Equal(t, http.StatusCreated, rec.Code)
	snap := decode[dto.SnapshotResponse](t, rec)
	assert.Equal(t, -1500.5, snap.TotalValueUsd)
	assert.Equal(t, 3000.0, snap.AssetsUsd)
	assert.Equal(t, 4500.5, snap.DebtsUsd)
	history := decode[[]dto.SnapshotResponse](t, do(t, r, "GET", "/api/v1/wealth/snapshots", nil))
	require.Len(t, history, 1)
	assert.Equal(t, 4500.5, history[0].DebtsUsd)

	// The projection grows the portfolio; debts are paid off on their own terms.
	createDebt(t, r, `{"name": "Visa", "balanceUsd": 1200, "monthlyPaymentUsd": 100}`)
	proj := decode[dto.ProjectionResponse](t, do(t, r, "GET", "/api/v1/wealth/estimate?contribution=0&yieldPct=0&years=1", nil))
	assert.Equal(t, 3000.0, proj.PrincipalUsd)
	assert.Equal(t, 1200.0, proj.DebtsUsd)
	assert.Equal(t, 1200.0, proj.Series[0].DebtBalanceUsd)
	assert.Equal(t, 1800.0, proj.Series[0].NetWorthUsd)
	assert.Equal(t, 0.0, proj.Series[1].DebtBalanceUsd)
	assert.Equal(t, 3000.0, proj.Series[1].NetWorthUsd)
}
