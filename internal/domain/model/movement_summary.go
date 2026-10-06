package model

import "github.com/shopspring/decimal"

// MovementGroup is a period's movements of one shape (their kind, whether they're about a debt, whether a
// holding is on the other end): how many, and the sums of their amounts, fees and value changes.
type MovementGroup struct {
	Kind MovementKind
	// OfDebt: they're about a debt (every DEBT_* kind, and a debt's OPENING, CLOSING or ADJUSTMENT).
	OfDebt bool
	// WithHolding: a holding is involved, as for a debt payment made from one or a charge that went into one.
	WithHolding bool
	Count       int
	Amount      decimal.Decimal
	Fee         decimal.Decimal
	// Change is Σ (new value − previous value): what corrections did to values (or to balances, a debt's).
	Change decimal.Decimal
}

// SummaryBucket is where a movement counts in a period's summary.
type SummaryBucket string

const (
	BucketGain                 SummaryBucket = "GAIN"
	BucketLoss                 SummaryBucket = "LOSS"
	BucketDeposit              SummaryBucket = "DEPOSIT"
	BucketWithdrawal           SummaryBucket = "WITHDRAWAL"
	BucketTransfer             SummaryBucket = "TRANSFER" // what was moved between holdings
	BucketTransferFees         SummaryBucket = "TRANSFER_FEES"
	BucketOpening              SummaryBucket = "OPENING" // holdings added
	BucketClosing              SummaryBucket = "CLOSING" // holdings removed
	BucketAdjustment           SummaryBucket = "ADJUSTMENT"
	BucketDebtOpening          SummaryBucket = "DEBT_OPENING"
	BucketDebtClosing          SummaryBucket = "DEBT_CLOSING"
	BucketDebtPaymentExternal  SummaryBucket = "DEBT_PAYMENT_EXTERNAL" // paid with money from outside
	BucketDebtPaymentFromAsset SummaryBucket = "DEBT_PAYMENT_FROM_ASSET"
	BucketDebtChargeExternal   SummaryBucket = "DEBT_CHARGE_EXTERNAL" // spent outside what's tracked
	BucketDebtChargeToAsset    SummaryBucket = "DEBT_CHARGE_TO_ASSET"
	BucketDebtInterest         SummaryBucket = "DEBT_INTEREST"
)

// SummaryBuckets are every bucket, in the order the API lists them.
var SummaryBuckets = []SummaryBucket{
	BucketGain, BucketLoss, BucketDeposit, BucketWithdrawal, BucketTransfer, BucketTransferFees,
	BucketOpening, BucketClosing, BucketAdjustment, BucketDebtOpening, BucketDebtClosing,
	BucketDebtPaymentExternal, BucketDebtPaymentFromAsset, BucketDebtChargeExternal, BucketDebtChargeToAsset,
	BucketDebtInterest,
}

// NetWorthEffect is a period's change of net worth as the movements recorded in it explain it, by why.
type NetWorthEffect struct {
	// Investments is what they earned: gains − losses − transfer fees − interest on debts.
	Investments decimal.Decimal
	// Saving is money in and out: deposits − withdrawals + debt payments made with money from outside −
	// debt charges spent outside what's tracked.
	Saving decimal.Decimal
	// AddedRemoved is what started or stopped being tracked: holdings added − removed + debts removed − added.
	AddedRemoved decimal.Decimal
	// Corrections is what fixing values and balances did.
	Corrections decimal.Decimal
}

// MovementsSummary is a period's movements in figures: how many, how many only moved money between what's
// owned and owed (Transfers: they leave the net worth as it was, but their fee), each bucket's total (the
// ADJUSTMENT one signed, as it changed the net worth), and what they did to the net worth.
type MovementsSummary struct {
	Count     int
	Transfers int
	Totals    map[SummaryBucket]decimal.Decimal
	Effect    NetWorthEffect
}
