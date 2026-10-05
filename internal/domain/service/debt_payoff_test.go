package service_test

import (
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var payoffNow = time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC)

func debt(balance float64, ratePct *float64, payment *float64) model.Debt {
	d := model.Debt{Balance: model.MustMoneyFromFloat(balance)}
	if ratePct != nil {
		rate := decimal.NewFromFloat(*ratePct)
		d.InterestRatePct = &rate
	}
	if payment != nil {
		p := model.MustMoneyFromFloat(*payment)
		d.MonthlyPayment = &p
	}
	return d
}

func f(v float64) *float64 { return &v }

func fixed(values []decimal.Decimal) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = v.StringFixed(2)
	}
	return out
}

func TestDebtPayoff_WithoutInterest(t *testing.T) {
	p := service.CalculateDebtPayoff(debt(1000, nil, f(300)), payoffNow)
	assert.Equal(t, model.PayoffOnTrack, p.Status)
	require.NotNil(t, p.Months)
	assert.Equal(t, 4, *p.Months, "300, 300, 300 and the last 100")
	assert.Equal(t, "2027-02", *p.PayoffMonth)
	assert.Equal(t, "0.00", p.TotalInterest.String())
}

func TestDebtPayoff_WithInterestRoundedEachMonth(t *testing.T) {
	// 12% a year is 1% a month: 10.00, 7.10, 4.17 and 1.21 of interest.
	p := service.CalculateDebtPayoff(debt(1000, f(12), f(300)), payoffNow)
	assert.Equal(t, model.PayoffOnTrack, p.Status)
	assert.Equal(t, 4, *p.Months)
	assert.Equal(t, "22.48", p.TotalInterest.String())
}

func TestDebtPayoff_PaidOffAndNoPayment(t *testing.T) {
	assert.Equal(t, model.DebtPayoff{Status: model.PayoffPaidOff}, service.CalculateDebtPayoff(debt(0, f(50), f(100)), payoffNow))
	assert.Equal(t, model.DebtPayoff{Status: model.PayoffNoPayment}, service.CalculateDebtPayoff(debt(500, f(50), nil), payoffNow))
	assert.Equal(t, model.DebtPayoff{Status: model.PayoffNoPayment}, service.CalculateDebtPayoff(debt(500, nil, f(0)), payoffNow))
}

func TestDebtPayoff_Never(t *testing.T) {
	// The payment only covers the interest (2% a month on 10,000): the balance never goes down.
	assert.Equal(t, model.DebtPayoff{Status: model.PayoffNever}, service.CalculateDebtPayoff(debt(10000, f(24), f(200)), payoffNow))
	assert.Equal(t, model.PayoffNever, service.CalculateDebtPayoff(debt(10000, f(24), f(150)), payoffNow).Status)
	// A cent above the interest: paid off, but in far more than 50 years.
	assert.Equal(t, model.PayoffNever, service.CalculateDebtPayoff(debt(100000, f(12), f(1000.01)), payoffNow).Status)
	// Just within the horizon.
	p := service.CalculateDebtPayoff(debt(600, nil, f(1)), payoffNow)
	assert.Equal(t, model.PayoffOnTrack, p.Status)
	assert.Equal(t, model.MaxPayoffMonths, *p.Months)
	assert.Equal(t, model.PayoffNever, service.CalculateDebtPayoff(debt(600.01, nil, f(1)), payoffNow).Status)
}

func TestDebtBalances(t *testing.T) {
	assert.Equal(t, []string{"1000.00", "710.00", "417.10", "121.27", "0.00", "0.00"},
		fixed(service.DebtBalances(debt(1000, f(12), f(300)), 5)))
	assert.Equal(t, []string{"500.00", "500.00", "500.00"}, fixed(service.DebtBalances(debt(500, f(80), nil), 2)),
		"without a payment it stays as it is")
	assert.Equal(t, []string{"1000.00", "1010.00", "1020.20"}, fixed(service.DebtBalances(debt(1000, f(24), f(10)), 2)),
		"a payment below the interest leaves it growing")
	huge := service.DebtBalances(debt(1e15, f(200), f(1)), 2)
	assert.Equal(t, "1000000000000000.00", huge[2].StringFixed(2), "never beyond what Money takes")
}

func TestTotalDebtBalances(t *testing.T) {
	assert.Nil(t, service.TotalDebtBalances(nil, 12))
	total := service.TotalDebtBalances([]model.Debt{debt(1000, nil, f(300)), debt(200, nil, nil)}, 2)
	assert.Equal(t, []string{"1200.00", "900.00", "600.00"}, fixed(total))
}

func TestMonthAfter(t *testing.T) {
	for months, want := range map[int]string{0: "2026-10", 2: "2026-12", 3: "2027-01", 15: "2028-01", 600: "2076-10"} {
		assert.Equal(t, want, service.MonthAfter(payoffNow, months), months)
	}
}

func TestProjection_NetWorthDiscountsDebts(t *testing.T) {
	params, err := model.NewProjectionParams(
		model.MustMoneyFromFloat(100000), model.ZeroMoney, decimal.Zero, 2,
		[]model.Money{model.MustMoneyFromFloat(50000), model.MustMoneyFromFloat(100000)},
	)
	require.NoError(t, err)
	// 60,000 owed, 1,000 paid off a month.
	params.DebtBalances = service.DebtBalances(debt(60000, nil, f(1000)), 24)

	series := service.CalculateSeries(params)
	require.Len(t, series, 3)
	assert.Equal(t, "60000.00", series[0].DebtBalance.String())
	assert.Equal(t, "40000.00", series[0].NetWorth.String())
	assert.Equal(t, "48000.00", series[1].DebtBalance.String())
	assert.Equal(t, "52000.00", series[1].NetWorth.String())
	assert.Equal(t, "100000.00", series[2].FutureValue.String(), "the portfolio itself doesn't change")

	milestones := service.CalculateMilestones(params, payoffNow)
	assert.Equal(t, model.MilestoneStatusReachable, milestones[0].Status, "the net worth reaches 50,000 once 10,000 is paid off")
	assert.Equal(t, 10, *milestones[0].MonthsRequired)
	assert.Equal(t, "2027-08", *milestones[0].TargetMonth)
	assert.Equal(t, model.MilestoneStatusOutOfHorizon, milestones[1].Status, "100,000 needs every debt paid off: 60 months")

	// Owing more than owned: the net worth starts below zero.
	params.DebtBalances = service.DebtBalances(debt(150000, nil, nil), 24)
	assert.Equal(t, "-50000.00", service.CalculateSeries(params)[0].NetWorth.String())
}

func TestProjection_WithoutDebtsTheNetWorthIsThePortfolio(t *testing.T) {
	params, err := model.NewProjectionParams(model.MustMoneyFromFloat(1000), model.MustMoneyFromFloat(100), decimal.Zero, 1, nil)
	require.NoError(t, err)
	last := service.CalculateSeries(params)[1]
	assert.True(t, last.DebtBalance.IsZero())
	assert.Equal(t, last.FutureValue.String(), last.NetWorth.String())
}
