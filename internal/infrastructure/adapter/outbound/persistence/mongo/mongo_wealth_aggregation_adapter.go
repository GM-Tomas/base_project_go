package mongo

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/GM-Tomas/base_project_go/internal/parallel"
	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type MongoWealthAggregationAdapter struct {
	holdingsColl  *mongo.Collection
	debtsColl     *mongo.Collection
	platformsColl *mongo.Collection // read-only, see legacyPlatformTypes
}

func NewMongoWealthAggregationAdapter(db *MongoDB) *MongoWealthAggregationAdapter {
	return &MongoWealthAggregationAdapter{holdingsColl: db.Holdings, debtsColl: db.Debts, platformsColl: db.Platforms}
}

// debtTotals is what the user owes, from their debts' balances and monthly payments. As with holdings
// (see readableValue), an amount that can't be read isn't counted.
func debtTotals(ctx context.Context, debts *mongo.Collection, userId model.UserId) (model.DebtTotals, error) {
	cursor, err := debts.Find(ctx, bson.M{"user_id": userId.UUID().String()},
		options.Find().SetProjection(bson.M{"balance_usd": 1, "monthly_payment_usd": 1}))
	if err != nil {
		return model.DebtTotals{}, err
	}
	var docs []debtDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return model.DebtTotals{}, err
	}
	totals := model.DebtTotals{Balance: model.ZeroMoney, MonthlyPayment: model.ZeroMoney}
	for _, doc := range docs {
		balance, err := readMoney(doc.BalanceUSD)
		if err != nil {
			continue
		}
		totals.Balance = totals.Balance.Plus(balance)
		totals.Count++
		if payment, err := readOptionalMoney(doc.MonthlyPaymentUSD); err == nil && payment != nil {
			totals.MonthlyPayment = totals.MonthlyPayment.Plus(*payment)
		}
	}
	return totals, nil
}

var _ outbound.WealthAggregationPort = (*MongoWealthAggregationAdapter)(nil)

// readableValue is a holding's amount, if it can be read: the totals (net worth, the breakdowns) count
// only those. Another amount can only have been written outside the API, and makes GET /holdings fail.
func readableValue(doc holdingDoc) (model.Money, bool) {
	number, err := decimal.NewFromString(doc.ValueUSD)
	if err != nil {
		return model.Money{}, false
	}
	value, err := model.NewMoney(number)
	return value, err == nil
}

// amount is a holding's amount, read once for every total it counts in.
type amount struct {
	value    model.Money
	readable bool // see readableValue
}

func amountsOf(docs []holdingDoc) []amount {
	amounts := make([]amount, len(docs))
	for i, doc := range docs {
		amounts[i].value, amounts[i].readable = readableValue(doc)
	}
	return amounts
}

// Totals reads what the user owns and what they owe, at once.
func (a *MongoWealthAggregationAdapter) Totals(ctx context.Context, userId model.UserId) (outbound.WealthTotals, error) {
	var (
		docs  []holdingDoc
		debts model.DebtTotals
	)
	err := parallel.Run(ctx,
		func(ctx context.Context) (err error) {
			docs, err = readHoldings(ctx, a.holdingsColl, userId, "value_usd")
			return err
		},
		func(ctx context.Context) (err error) {
			debts, err = debtTotals(ctx, a.debtsColl, userId)
			return err
		},
	)
	if err != nil {
		return outbound.WealthTotals{}, err
	}
	return outbound.WealthTotals{Assets: assetsOf(amountsOf(docs)), Debts: debts.Balance}, nil
}

// Breakdown reads the user's holdings once, alongside the platform types earlier versions stored, for
// the assets and both breakdowns, and their debts at the same time.
func (a *MongoWealthAggregationAdapter) Breakdown(
	ctx context.Context,
	userId model.UserId,
) (outbound.WealthBreakdown, error) {
	var (
		docs  []holdingDoc
		types map[string]model.PlatformType
		debts model.DebtTotals
	)
	err := parallel.Run(ctx,
		func(ctx context.Context) (err error) {
			docs, types, err = holdingsWithTypes(ctx, a.holdingsColl, a.platformsColl, userId,
				"asset_class", "platform_name", "value_usd")
			return err
		},
		func(ctx context.Context) (err error) {
			debts, err = debtTotals(ctx, a.debtsColl, userId)
			return err
		},
	)
	if err != nil {
		return outbound.WealthBreakdown{}, err
	}
	amounts := amountsOf(docs)
	return outbound.WealthBreakdown{
		Assets:       assetsOf(amounts),
		Debts:        debts,
		ByAssetClass: classBreakdown(docs, amounts),
		ByPlatform:   platformBreakdown(groupPlatforms(docs, amounts), types),
	}, nil
}

func assetsOf(amounts []amount) model.Money {
	total := model.ZeroMoney
	for _, a := range amounts {
		if a.readable {
			total = total.Plus(a.value)
		}
	}
	return total
}

func classBreakdown(docs []holdingDoc, amounts []amount) []outbound.AssetClassAggregate {
	type classAccumulator struct {
		total model.Money
		count int
	}

	// Keyed by the class as the domain reads it, so stored spellings it reads as one (different Unicode
	// forms, say) are one class, as in GET /holdings.
	accMap := make(map[model.AssetClass]*classAccumulator)
	for i, doc := range docs {
		if !amounts[i].readable {
			continue
		}
		ac, err := model.NewAssetClass(doc.AssetClass)
		if err != nil {
			continue
		}

		acc, exists := accMap[ac]
		if !exists {
			acc = &classAccumulator{total: model.ZeroMoney, count: 0}
			accMap[ac] = acc
		}
		acc.total = acc.total.Plus(amounts[i].value)
		acc.count++
	}

	type classRow struct {
		outbound.AssetClassAggregate
		sortName string
	}
	rows := make([]classRow, 0, len(accMap))
	for ac, acc := range accMap {
		rows = append(rows, classRow{
			AssetClassAggregate: outbound.AssetClassAggregate{AssetClass: ac, Value: acc.total, Count: acc.count},
			sortName:            sortName(ac.Value()),
		})
	}

	// By value, largest first, then by name as platforms are (classes differing only in case stay apart).
	slices.SortFunc(rows, func(x, y classRow) int {
		return cmp.Or(
			y.Value.Amount().Cmp(x.Value.Amount()),
			strings.Compare(x.sortName, y.sortName),
			strings.Compare(x.AssetClass.Value(), y.AssetClass.Value()),
		)
	})
	var list []outbound.AssetClassAggregate
	for _, row := range rows {
		list = append(list, row.AssetClassAggregate)
	}
	return list
}

// platformBreakdown lists the platforms of PlatformRepository.FindAll, named and typed the same way,
// leaving out those without a readable amount, like the class breakdown (see readableValue).
func platformBreakdown(groups map[string]*platformGroup, types map[string]model.PlatformType) []outbound.PlatformAggregate {
	// By value, largest first, then alphabetically.
	byValue := func(x, y *platformGroup) int {
		return cmp.Or(y.total.Amount().Cmp(x.total.Amount()), byName(x, y))
	}
	list := make([]outbound.PlatformAggregate, 0, len(groups))
	for _, g := range slices.SortedFunc(maps.Values(groups), byValue) {
		if g.count == 0 {
			continue
		}
		list = append(list, outbound.PlatformAggregate{
			Name:  g.name,
			Type:  typeOf(types, g.key),
			Value: g.total,
			Count: g.count,
		})
	}
	return list
}
