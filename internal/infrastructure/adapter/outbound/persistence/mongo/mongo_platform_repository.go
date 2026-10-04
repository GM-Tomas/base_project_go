package mongo

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoPlatformRepository derives the user's platforms from their holdings: a platform is a name holdings
// use, matched case-insensitively. Nothing is stored for it, so nothing can drift out of sync with them.
type MongoPlatformRepository struct {
	holdingsColl *mongo.Collection
}

func NewMongoPlatformRepository(db *MongoDB) *MongoPlatformRepository {
	return &MongoPlatformRepository{holdingsColl: db.Holdings}
}

var _ outbound.PlatformRepository = (*MongoPlatformRepository)(nil)

// FindAll lists the user's platforms sorted by name, each created when its first holding was.
func (r *MongoPlatformRepository) FindAll(ctx context.Context, userId model.UserId) ([]model.Platform, error) {
	groups, err := platformGroups(ctx, r.holdingsColl, userId)
	if err != nil {
		return nil, err
	}
	platforms := make([]model.Platform, 0, len(groups))
	for _, g := range groups {
		platforms = append(platforms, model.Platform{
			UserId:    userId,
			Name:      g.name,
			Type:      model.PlatformTypeOther,
			CreatedAt: g.firstUsed,
		})
	}
	sort.Slice(platforms, func(i, j int) bool { return platforms[i].Name.Value() < platforms[j].Name.Value() })
	return platforms, nil
}

// Canonical returns the spelling the user's holdings already use for this platform, or name itself for a
// new one. (Two creates racing on a new platform in different cases can each keep their spelling; every
// view groups them case-insensitively all the same.)
func (r *MongoPlatformRepository) Canonical(
	ctx context.Context,
	userId model.UserId,
	name model.PlatformName,
) (model.PlatformName, error) {
	groups, err := platformGroups(ctx, r.holdingsColl, userId)
	if err != nil {
		return model.PlatformName{}, err
	}
	if g, ok := groups[platformKey(name)]; ok {
		return g.name, nil
	}
	return name, nil
}

// platformGroup is one platform: the holdings whose platform names match case-insensitively.
type platformGroup struct {
	name      model.PlatformName // as spelled on the group's earliest holding
	firstUsed time.Time
	total     model.Money // value of the holdings with a valid amount, like NetWorth
	count     int
}

func platformKey(name model.PlatformName) string { return strings.ToLower(name.Value()) }

// platformGroups groups the user's holdings by platform, keyed by platformKey. Holdings whose stored
// platform name the domain rejects (only writable outside the API) are left out.
func platformGroups(ctx context.Context, holdings *mongo.Collection, userId model.UserId) (map[string]*platformGroup, error) {
	cursor, err := holdings.Find(ctx,
		bson.M{"user_id": userId.UUID().String()},
		options.Find().
			SetProjection(bson.M{"platform_name": 1, "created_at": 1, "value_usd": 1}).
			SetSort(bson.D{{Key: "created_at", Value: 1}, {Key: "_id", Value: 1}}),
	)
	if err != nil {
		return nil, err
	}
	var docs []holdingDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	groups := make(map[string]*platformGroup)
	for _, doc := range docs {
		name, err := model.NewPlatformName(doc.PlatformName)
		if err != nil {
			continue
		}
		g, ok := groups[platformKey(name)]
		if !ok {
			g = &platformGroup{name: name, firstUsed: doc.CreatedAt, total: model.ZeroMoney}
			groups[platformKey(name)] = g
		}
		amount, err := decimal.NewFromString(doc.ValueUSD)
		if err != nil {
			continue
		}
		value, err := model.NewMoney(amount)
		if err != nil {
			continue
		}
		g.total = g.total.Plus(value)
		g.count++
	}
	return groups, nil
}
