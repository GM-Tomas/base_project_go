package mongo

import (
	"context"
	"sort"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type MongoWealthAggregationAdapter struct {
	holdingsColl  *mongo.Collection
	platformsColl *mongo.Collection
}

func NewMongoWealthAggregationAdapter(db *MongoDB) *MongoWealthAggregationAdapter {
	return &MongoWealthAggregationAdapter{
		holdingsColl:  db.Holdings,
		platformsColl: db.Platforms,
	}
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

	accMap := make(map[string]*classAccumulator)
	for _, doc := range docs {
		valDec, err := decimal.NewFromString(doc.ValueUSD)
		if err != nil {
			continue
		}
		m, err := model.NewMoney(valDec)
		if err != nil {
			continue
		}

		acc, exists := accMap[doc.AssetClass]
		if !exists {
			acc = &classAccumulator{total: model.ZeroMoney, count: 0}
			accMap[doc.AssetClass] = acc
		}
		acc.total = acc.total.Plus(m)
		acc.count++
	}

	var list []outbound.AssetClassAggregate
	for className, acc := range accMap {
		ac, err := model.NewAssetClass(className)
		if err != nil {
			continue
		}
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
	// 1. Fetch all platforms of this user (including those with 0 holdings)
	pFilter := bson.M{"user_id": userId.UUID().String()}
	pCursor, err := a.platformsColl.Find(ctx, pFilter)
	if err != nil {
		return nil, err
	}
	defer pCursor.Close(ctx)

	var pDocs []platformDoc
	if err := pCursor.All(ctx, &pDocs); err != nil {
		return nil, err
	}

	// 2. Fetch all holdings of this user
	hFilter := bson.M{"user_id": userId.UUID().String()}
	hCursor, err := a.holdingsColl.Find(ctx, hFilter)
	if err != nil {
		return nil, err
	}
	defer hCursor.Close(ctx)

	var hDocs []holdingDoc
	if err := hCursor.All(ctx, &hDocs); err != nil {
		return nil, err
	}

	type platformStats struct {
		total model.Money
		count int
	}
	statsByPlatform := make(map[string]*platformStats)
	for _, doc := range hDocs {
		valDec, err := decimal.NewFromString(doc.ValueUSD)
		if err != nil {
			continue
		}
		m, err := model.NewMoney(valDec)
		if err != nil {
			continue
		}

		ps, exists := statsByPlatform[doc.PlatformName]
		if !exists {
			ps = &platformStats{total: model.ZeroMoney, count: 0}
			statsByPlatform[doc.PlatformName] = ps
		}
		ps.total = ps.total.Plus(m)
		ps.count++
	}

	var list []outbound.PlatformAggregate
	for _, pDoc := range pDocs {
		pn, err := model.NewPlatformName(pDoc.Name)
		if err != nil {
			continue
		}
		pt, err := model.NewPlatformType(pDoc.Type)
		if err != nil {
			continue
		}

		total := model.ZeroMoney
		count := 0
		if ps, found := statsByPlatform[pDoc.Name]; found {
			total = ps.total
			count = ps.count
		}

		list = append(list, outbound.PlatformAggregate{
			Name:  pn,
			Type:  pt,
			Value: total,
			Count: count,
		})
	}

	// Sort descending by value, then alphabetically by platform name
	sort.Slice(list, func(i, j int) bool {
		cmp := list[i].Value.Amount().Cmp(list[j].Value.Amount())
		if cmp != 0 {
			return cmp > 0
		}
		return list[i].Name.Value() < list[j].Name.Value()
	})

	return list, nil
}
