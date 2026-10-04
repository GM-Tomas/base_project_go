package mongo

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type MongoWealthAggregationAdapter struct {
	holdingsColl  *mongo.Collection
	platformsColl *mongo.Collection // read-only, see legacyPlatformTypes
}

func NewMongoWealthAggregationAdapter(db *MongoDB) *MongoWealthAggregationAdapter {
	return &MongoWealthAggregationAdapter{holdingsColl: db.Holdings, platformsColl: db.Platforms}
}

var _ outbound.WealthAggregationPort = (*MongoWealthAggregationAdapter)(nil)

// readableValue is a holding's amount, if it can be read: the totals (net worth, the breakdowns) count
// only those. Another amount can only have been written outside the API, and makes GET /holdings fail.
func readableValue(doc holdingDoc) (model.Money, bool) {
	amount, err := decimal.NewFromString(doc.ValueUSD)
	if err != nil {
		return model.Money{}, false
	}
	value, err := model.NewMoney(amount)
	return value, err == nil
}

func (a *MongoWealthAggregationAdapter) NetWorth(
	ctx context.Context,
	userId model.UserId,
) (model.Money, error) {
	docs, err := readHoldings(ctx, a.holdingsColl, userId, "value_usd")
	if err != nil {
		return model.ZeroMoney, err
	}
	return netWorthOf(docs), nil
}

// Breakdown reads the user's holdings once, alongside the platform types earlier versions stored, for
// the net worth and both breakdowns.
func (a *MongoWealthAggregationAdapter) Breakdown(
	ctx context.Context,
	userId model.UserId,
) (outbound.WealthBreakdown, error) {
	docs, types, err := holdingsWithTypes(ctx, a.holdingsColl, a.platformsColl, userId,
		"asset_class", "platform_name", "created_at", "value_usd")
	if err != nil {
		return outbound.WealthBreakdown{}, err
	}
	return outbound.WealthBreakdown{
		NetWorth:     netWorthOf(docs),
		ByAssetClass: classBreakdown(docs),
		ByPlatform:   platformBreakdown(groupPlatforms(docs, true), types),
	}, nil
}

func netWorthOf(docs []holdingDoc) model.Money {
	total := model.ZeroMoney
	for _, doc := range docs {
		if value, ok := readableValue(doc); ok {
			total = total.Plus(value)
		}
	}
	return total
}

func classBreakdown(docs []holdingDoc) []outbound.AssetClassAggregate {
	type classAccumulator struct {
		total model.Money
		count int
	}

	// Keyed by the class as the domain reads it, so stored spellings it reads as one (different Unicode
	// forms, say) are one class, as in GET /holdings.
	accMap := make(map[model.AssetClass]*classAccumulator)
	for _, doc := range docs {
		m, ok := readableValue(doc)
		if !ok {
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
		acc.total = acc.total.Plus(m)
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
