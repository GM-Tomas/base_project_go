package service_test

import (
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func d(v string) decimal.Decimal { return decimal.RequireFromString(v) }

func group(kind model.MovementKind, count int, amount string) model.MovementGroup {
	return model.MovementGroup{Kind: kind, Count: count, Amount: d(amount), Fee: decimal.Zero, Change: decimal.Zero}
}

func TestMovementsEffect_EachKindCountsWhereItBelongs(t *testing.T) {
	transfer := group(model.MovementTransfer, 4, "2000")
	transfer.Fee = d("12")
	debtOpening := group(model.MovementOpening, 1, "500")
	debtOpening.OfDebt = true
	debtClosing := group(model.MovementClosing, 1, "80")
	debtClosing.OfDebt = true
	holdingFix := model.MovementGroup{Kind: model.MovementAdjustment, Count: 2, Change: d("70")}
	debtFix := model.MovementGroup{Kind: model.MovementAdjustment, Count: 1, OfDebt: true, Change: d("25")} // owed 25 more
	paidFromAsset := group(model.MovementDebtPayment, 2, "900")
	paidFromAsset.OfDebt, paidFromAsset.WithHolding = true, true
	paidFromOutside := group(model.MovementDebtPayment, 1, "300")
	paidFromOutside.OfDebt = true
	chargedIntoAsset := group(model.MovementDebtCharge, 1, "1000")
	chargedIntoAsset.OfDebt, chargedIntoAsset.WithHolding = true, true
	spent := group(model.MovementDebtCharge, 3, "150")
	spent.OfDebt = true
	interest := group(model.MovementDebtInterest, 2, "40")
	interest.OfDebt = true

	summary := service.MovementsEffect([]model.MovementGroup{
		group(model.MovementGain, 5, "6000"), group(model.MovementLoss, 2, "900"),
		group(model.MovementDeposit, 3, "9000"), group(model.MovementWithdrawal, 1, "1000"),
		transfer, group(model.MovementOpening, 2, "3000"), group(model.MovementClosing, 1, "700"),
		debtOpening, debtClosing, holdingFix, debtFix, paidFromAsset, paidFromOutside, chargedIntoAsset, spent, interest,
	})

	assert.Equal(t, 32, summary.Count)
	assert.Equal(t, 7, summary.Transfers, "4 transfers, 2 payments from a holding, 1 charge into one")
	want := map[model.SummaryBucket]string{
		"GAIN": "6000", "LOSS": "900", "DEPOSIT": "9000", "WITHDRAWAL": "1000", "TRANSFER": "2000", "TRANSFER_FEES": "12",
		"OPENING": "3000", "CLOSING": "700", "ADJUSTMENT": "45", "DEBT_OPENING": "500", "DEBT_CLOSING": "80",
		"DEBT_PAYMENT_EXTERNAL": "300", "DEBT_PAYMENT_FROM_ASSET": "900", "DEBT_CHARGE_EXTERNAL": "150",
		"DEBT_CHARGE_TO_ASSET": "1000", "DEBT_INTEREST": "40",
	}
	assert.Len(t, summary.Totals, len(model.SummaryBuckets))
	for bucket, total := range want {
		assert.True(t, d(total).Equal(summary.Totals[bucket]), "%s: %s", bucket, summary.Totals[bucket])
	}
	// 6000 − 900 − 12 − 40; 9000 − 1000 + 300 − 150; 3000 − 700 + 80 − 500; 70 − 25.
	assert.Equal(t, "5048", summary.Effect.Investments.String())
	assert.Equal(t, "8150", summary.Effect.Saving.String())
	assert.Equal(t, "1880", summary.Effect.AddedRemoved.String())
	assert.Equal(t, "45", summary.Effect.Corrections.String())
}

func TestMovementsEffect_NothingRecorded(t *testing.T) {
	summary := service.MovementsEffect(nil)
	assert.Zero(t, summary.Count)
	assert.Zero(t, summary.Transfers)
	for _, b := range model.SummaryBuckets {
		assert.True(t, summary.Totals[b].IsZero(), b)
	}
	assert.True(t, summary.Effect.Investments.IsZero())
	assert.True(t, summary.Effect.Saving.IsZero())
	assert.True(t, summary.Effect.AddedRemoved.IsZero())
	assert.True(t, summary.Effect.Corrections.IsZero())

	// A kind it doesn't know (only writable outside the API) is counted, and changes nothing.
	unknown := service.MovementsEffect([]model.MovementGroup{group("SPLIT", 1, "10")})
	assert.Equal(t, 1, unknown.Count)
	assert.True(t, unknown.Effect.Saving.IsZero())
}
