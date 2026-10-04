package mongo

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"sort"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/bson"
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

func (a *MongoWealthAggregationAdapter) NetWorth(
	ctx context.Context,
	userId model.UserId,
) (model.Money, error) {
	filter := bson.M{"user_id": userId.UUID().String()}
	cursor, err := a.holdingsColl.Find(ctx, filter)
	if err != nil {
		return model.ZeroMoney, err
	}
	defer cursor.Close(ctx)

	var docs []holdingDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return model.ZeroMoney, err
	}

	var totalMoney = model.ZeroMoney
	for _, doc := range docs {
		valDec, err := decimal.NewFromString(doc.ValueUSD)
		if err == nil {
			m, err := model.NewMoney(valDec)
			if err == nil {
				totalMoney = totalMoney.Plus(m)
			}
		}
	}

	return totalMoney, nil
}

func (a *MongoWealthAggregationAdapter) ByAssetClass(
	ctx context.Context,
	userId model.UserId,
) ([]outbound.AssetClassAggregate, error) {
	filter := bson.M{"user_id": userId.UUID().String()}
	cursor, err := a.holdingsColl.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []holdingDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	type classAccumulator struct {
		total model.Money
		count int
	}

	// Keyed by the class as the domain reads it, so stored spellings it reads as one (different Unicode
	// forms, say) are one class, as in GET /holdings.
	accMap := make(map[model.AssetClass]*classAccumulator)
	for _, doc := range docs {
		valDec, err := decimal.NewFromString(doc.ValueUSD)
		if err != nil {
			continue
		}
		m, err := model.NewMoney(valDec)
		if err != nil {
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

	var list []outbound.AssetClassAggregate
	for ac, acc := range accMap {
		list = append(list, outbound.AssetClassAggregate{
			AssetClass: ac,
			Value:      acc.total,
			Count:      acc.count,
		})
	}

	// Sort descending by value, then ascending by class name
	sort.Slice(list, func(i, j int) bool {
		cmp := list[i].Value.Amount().Cmp(list[j].Value.Amount())
		if cmp != 0 {
			return cmp > 0
		}
		return list[i].AssetClass.Value() < list[j].AssetClass.Value()
	})

	return list, nil
}

func (a *MongoWealthAggregationAdapter) ByPlatform(
	ctx context.Context,
	userId model.UserId,
) ([]outbound.PlatformAggregate, error) {
	// The same platforms as PlatformRepository.FindAll, so the breakdown matches the platform list.
	groups, types, err := platformsWithTypes(ctx, a.holdingsColl, a.platformsColl, userId)
	if err != nil {
		return nil, err
	}

	// By value, largest first, then alphabetically.
	byValue := func(x, y *platformGroup) int {
		return cmp.Or(y.total.Amount().Cmp(x.total.Amount()), byName(x, y))
	}
	list := make([]outbound.PlatformAggregate, 0, len(groups))
	for _, g := range slices.SortedFunc(maps.Values(groups), byValue) {
		list = append(list, outbound.PlatformAggregate{
			Name:  g.name,
			Type:  typeOf(types, g.key),
			Value: g.total,
			Count: g.count,
		})
	}

	return list, nil
}
