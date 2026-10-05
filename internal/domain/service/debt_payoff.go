package service

import (
	"fmt"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/shopspring/decimal"
)

var (
	twelveHundred = decimal.NewFromInt(1200)
	// maxBalance bounds a balance that grows (a payment below the interest): the largest amount Money takes.
	maxBalance = decimal.New(1, 15)
)

// monthlyRate is a debt's interest per month, as a fraction: annual percent / 12 / 100.
func monthlyRate(d model.Debt) decimal.Decimal {
	if d.InterestRatePct == nil {
		return decimal.Zero
	}
	return d.InterestRatePct.Div(twelveHundred)
}

// amortize is one month of a debt: its interest (rounded to cents, as lenders charge it) is added, then the
// payment taken off.
func amortize(balance, rate, payment decimal.Decimal) (next, interest decimal.Decimal) {
	interest = balance.Mul(rate).Round(2)
	return balance.Add(interest).Sub(payment), interest
}

// CalculateDebtPayoff is when the debt is paid off at its monthly payment: balance ← balance × (1 + rate/12)
// − payment, month by month, until nothing is left (the last payment is whatever remains). A payment that
// doesn't cover the first month's interest never pays it off, nor does one that would take more than
// model.MaxPayoffMonths.
func CalculateDebtPayoff(d model.Debt, now time.Time) model.DebtPayoff {
	if d.Balance.IsZero() {
		return model.DebtPayoff{Status: model.PayoffPaidOff}
	}
	if d.MonthlyPayment == nil || d.MonthlyPayment.IsZero() {
		return model.DebtPayoff{Status: model.PayoffNoPayment}
	}
	rate, payment := monthlyRate(d), d.MonthlyPayment.Amount()
	balance, total := d.Balance.Amount(), decimal.Zero
	for month := 1; month <= model.MaxPayoffMonths; month++ {
		next, interest := amortize(balance, rate, payment)
		if next.GreaterThanOrEqual(balance) {
			break // the balance never goes down
		}
		balance, total = next, total.Add(interest)
		if !balance.IsPositive() {
			months, payoffMonth, totalInterest := month, MonthAfter(now, month), model.MustMoney(total)
			return model.DebtPayoff{
				Status: model.PayoffOnTrack, Months: &months, PayoffMonth: &payoffMonth, TotalInterest: &totalInterest,
			}
		}
	}
	return model.DebtPayoff{Status: model.PayoffNever}
}

// DebtBalances is what's left to pay now and after each of the next months (months+1 values), amortized as
// CalculateDebtPayoff does and never below zero. Without a monthly payment it stays as it is; with one that
// doesn't cover the interest it grows, up to the largest amount Money takes.
func DebtBalances(d model.Debt, months int) []decimal.Decimal {
	balances := make([]decimal.Decimal, months+1)
	balance := d.Balance.Amount()
	balances[0] = balance
	if d.MonthlyPayment == nil || d.MonthlyPayment.IsZero() {
		for m := 1; m <= months; m++ {
			balances[m] = balance
		}
		return balances
	}
	rate, payment := monthlyRate(d), d.MonthlyPayment.Amount()
	for m := 1; m <= months; m++ {
		if balance.IsPositive() {
			balance, _ = amortize(balance, rate, payment)
			balance = decimal.Max(decimal.Min(balance, maxBalance), decimal.Zero)
		}
		balances[m] = balance
	}
	return balances
}

// TotalDebtBalances is what all the debts add up to now and after each of the next months (months+1
// values); nil without debts.
func TotalDebtBalances(debts []model.Debt, months int) []decimal.Decimal {
	if len(debts) == 0 {
		return nil
	}
	total := make([]decimal.Decimal, months+1)
	for _, d := range debts {
		for m, balance := range DebtBalances(d, months) {
			total[m] = total[m].Add(balance)
		}
	}
	return total
}

// MonthAfter is the calendar month months after now's ("YYYY-MM"), in UTC.
func MonthAfter(now time.Time, months int) string {
	totalMonths := int(now.UTC().Month()) - 1 + months
	return fmt.Sprintf("%04d-%02d", now.UTC().Year()+totalMonths/12, totalMonths%12+1)
}
