package service

import (
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/shopspring/decimal"
)

// MovementsEffect sums a period's movements, grouped by shape, by bucket, and says what they did to the net
// worth (see model.NetWorthEffect). Money moved between holdings, or between a holding and a debt (a
// payment made from one, a charge that went into one), leaves the net worth as it was, but a transfer's
// fee: those are only counted.
func MovementsEffect(groups []model.MovementGroup) model.MovementsSummary {
	totals := make(map[model.SummaryBucket]decimal.Decimal, len(model.SummaryBuckets))
	for _, b := range model.SummaryBuckets {
		totals[b] = decimal.Zero
	}
	add := func(b model.SummaryBucket, v decimal.Decimal) { totals[b] = totals[b].Add(v) }
	either := func(debt bool, ofDebt, ofHolding model.SummaryBucket) model.SummaryBucket {
		if debt {
			return ofDebt
		}
		return ofHolding
	}

	summary := model.MovementsSummary{}
	for _, g := range groups {
		summary.Count += g.Count
		switch g.Kind {
		case model.MovementOpening:
			add(either(g.OfDebt, model.BucketDebtOpening, model.BucketOpening), g.Amount)
		case model.MovementClosing:
			add(either(g.OfDebt, model.BucketDebtClosing, model.BucketClosing), g.Amount)
		case model.MovementGain:
			add(model.BucketGain, g.Amount)
		case model.MovementLoss:
			add(model.BucketLoss, g.Amount)
		case model.MovementDeposit:
			add(model.BucketDeposit, g.Amount)
		case model.MovementWithdrawal:
			add(model.BucketWithdrawal, g.Amount)
		case model.MovementTransfer:
			add(model.BucketTransfer, g.Amount)
			add(model.BucketTransferFees, g.Fee)
			summary.Transfers += g.Count
		case model.MovementAdjustment:
			// A debt's balance going up lowers the net worth.
			if g.OfDebt {
				add(model.BucketAdjustment, g.Change.Neg())
			} else {
				add(model.BucketAdjustment, g.Change)
			}
		case model.MovementDebtPayment:
			add(either(g.WithHolding, model.BucketDebtPaymentFromAsset, model.BucketDebtPaymentExternal), g.Amount)
			if g.WithHolding {
				summary.Transfers += g.Count
			}
		case model.MovementDebtCharge:
			add(either(g.WithHolding, model.BucketDebtChargeToAsset, model.BucketDebtChargeExternal), g.Amount)
			if g.WithHolding {
				summary.Transfers += g.Count
			}
		case model.MovementDebtInterest:
			add(model.BucketDebtInterest, g.Amount)
		}
	}

	t := totals
	summary.Totals = totals
	summary.Effect = model.NetWorthEffect{
		Investments:  t[model.BucketGain].Sub(t[model.BucketLoss]).Sub(t[model.BucketTransferFees]).Sub(t[model.BucketDebtInterest]),
		Saving:       t[model.BucketDeposit].Sub(t[model.BucketWithdrawal]).Add(t[model.BucketDebtPaymentExternal]).Sub(t[model.BucketDebtChargeExternal]),
		AddedRemoved: t[model.BucketOpening].Sub(t[model.BucketClosing]).Add(t[model.BucketDebtClosing]).Sub(t[model.BucketDebtOpening]),
		Corrections:  t[model.BucketAdjustment],
	}
	return summary
}
