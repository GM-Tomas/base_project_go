package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMovementService_SummarizesAPeriod(t *testing.T) {
	a := newAccount(t)
	ctx := context.Background()
	savings := a.add("Savings", "Bank", 1000)
	broker := a.add("Fund", "Broker", 500)
	visa, err := a.debtSvc.CreateDebt(ctx, inbound.CreateDebtCommand{UserId: a.user, Name: "Visa", Kind: "CREDIT_CARD", BalanceUsd: 300})
	require.NoError(t, err)
	lastMonth := movementNow.AddDate(0, -1, 0)
	for _, cmd := range []inbound.RecordMovementCommand{
		{Kind: "GAIN", HoldingId: &broker.Id, AmountUsd: 60},
		{Kind: "LOSS", HoldingId: &broker.Id, AmountUsd: 10},
		{Kind: "DEPOSIT", HoldingId: &savings.Id, AmountUsd: 400},
		{Kind: "WITHDRAWAL", HoldingId: &savings.Id, AmountUsd: 100, OccurredAt: &lastMonth},
		{Kind: "TRANSFER", FromHoldingId: &savings.Id, ToHoldingId: &broker.Id, AmountUsd: 200, FeeUsd: 2},
		{Kind: "DEBT_PAYMENT", DebtId: &visa.Debt.Id, FromHoldingId: &savings.Id, AmountUsd: 100},
		{Kind: "DEBT_PAYMENT", DebtId: &visa.Debt.Id, AmountUsd: 50},
		{Kind: "DEBT_CHARGE", DebtId: &visa.Debt.Id, AmountUsd: 80},
		{Kind: "DEBT_INTEREST", DebtId: &visa.Debt.Id, AmountUsd: 5},
	} {
		_, err := a.record(cmd)
		require.NoError(t, err, cmd.Kind)
	}
	// A correction: the fund was worth 20 less than it said.
	_, err = a.holdingSvc.UpdateHolding(ctx, inbound.UpdateHoldingCommand{UserId: a.user, Id: broker.Id, ValueUsd: ptr(728.0), ValueChangeReason: "CORRECTION"})
	require.NoError(t, err)

	all, err := a.movementSvc.SummarizeMovements(ctx, a.user, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), all.From)
	assert.Equal(t, movementNow, all.To)
	s := all.Summary
	assert.Equal(t, 13, s.Count) // 2 holdings and a debt added, 9 recorded, 1 correction
	assert.Equal(t, 2, s.Transfers)
	assert.Equal(t, "1500", s.Totals[model.BucketOpening].String())
	assert.Equal(t, "300", s.Totals[model.BucketDebtOpening].String())
	assert.Equal(t, "-20", s.Totals[model.BucketAdjustment].String())
	assert.Equal(t, "43", s.Effect.Investments.String())    // 60 − 10 − 2 − 5
	assert.Equal(t, "270", s.Effect.Saving.String())        // 400 − 100 + 50 − 80
	assert.Equal(t, "1200", s.Effect.AddedRemoved.String()) // 1500 − 300
	assert.Equal(t, "-20", s.Effect.Corrections.String())

	// Since last week: the withdrawal of last month is out.
	lastWeek := movementNow.AddDate(0, 0, -7)
	recent, err := a.movementSvc.SummarizeMovements(ctx, a.user, &lastWeek, nil)
	require.NoError(t, err)
	assert.Equal(t, "370", recent.Summary.Effect.Saving.String())

	// Someone else's are theirs.
	other := newAccount(t)
	theirs, err := a.movementSvc.SummarizeMovements(ctx, other.user, nil, nil)
	require.NoError(t, err)
	assert.Zero(t, theirs.Summary.Count)

	// A period that ends before it starts.
	_, err = a.movementSvc.SummarizeMovements(ctx, a.user, &movementNow, &lastWeek)
	var invalid appErrors.ValidationErrors
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "from must not be after to", invalid.Errors[0].Message)

	a.movements.listErr = assert.AnError
	_, err = a.movementSvc.SummarizeMovements(ctx, a.user, nil, nil)
	assert.ErrorIs(t, err, assert.AnError)
}
