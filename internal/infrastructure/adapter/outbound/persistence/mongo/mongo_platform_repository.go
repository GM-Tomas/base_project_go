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
// use, matched case-insensitively and spelled as on its earliest holding. Nothing is written for it, so
// nothing can drift out of sync with them.
type MongoPlatformRepository struct {
	holdingsColl  *mongo.Collection
	platformsColl *mongo.Collection // read-only, see legacyPlatformTypes
}

func NewMongoPlatformRepository(db *MongoDB) *MongoPlatformRepository {
	return &MongoPlatformRepository{holdingsColl: db.Holdings, platformsColl: db.Platforms}
}

var _ outbound.PlatformRepository = (*MongoPlatformRepository)(nil)

// FindAll lists the user's platforms alphabetically, each created when its first holding was.
func (r *MongoPlatformRepository) FindAll(ctx context.Context, userId model.UserId) ([]model.Platform, error) {
	groups, err := platformGroups(ctx, r.holdingsColl, userId)
	if err != nil {
		return nil, err
	}
	types, err := legacyPlatformTypes(ctx, r.platformsColl, userId)
	if err != nil {
		return nil, err
	}
	platforms := make([]model.Platform, 0, len(groups))
	for key, g := range groups {
		platforms = append(platforms, model.Platform{
			UserId:    userId,
			Name:      g.name,
			Type:      typeOf(types, key),
			CreatedAt: g.firstUsed,
		})
	}
	sort.Slice(platforms, func(i, j int) bool { return lessPlatformName(platforms[i].Name, platforms[j].Name) })
	return platforms, nil
}

// Canonical returns the spelling the user's holdings already use for this platform, or name itself for a
// new one. (Two creates racing on a new platform in different cases can each keep their spelling; every
// view treats them as one, spelled as the earliest.)
func (r *MongoPlatformRepository) Canonical(
	ctx context.Context,
	userId model.UserId,
	name model.PlatformName,
) (model.PlatformName, error) {
	uid := userId.UUID().String()
	var used []string
	if err := r.holdingsColl.Distinct(ctx, "platform_name", bson.M{"user_id": uid}).Decode(&used); err != nil {
		return model.PlatformName{}, err
	}
	var spellings []string
	for _, s := range used {
		if strings.ToLower(s) == platformKey(name) {
			spellings = append(spellings, s)
		}
	}
	switch len(spellings) {
	case 0:
		return name, nil
	case 1:
		return model.NewPlatformName(spellings[0])
	}
	var earliest holdingDoc
	err := r.holdingsColl.FindOne(ctx,
		bson.M{"user_id": uid, "platform_name": bson.M{"$in": spellings}},
		options.FindOne().
			SetSort(bson.D{{Key: "created_at", Value: 1}, {Key: "_id", Value: 1}}).
			SetProjection(bson.M{"platform_name": 1}),
	).Decode(&earliest)
	if err != nil {
		return model.PlatformName{}, err
	}
	return model.NewPlatformName(earliest.PlatformName)
}

// platformGroup is one platform: the holdings whose platform names match case-insensitively.
type platformGroup struct {
	name      model.PlatformName // as spelled on the group's earliest holding
	firstUsed time.Time
	total     model.Money // value of the holdings with a valid amount, like NetWorth
	count     int
}

func platformKey(name model.PlatformName) string { return strings.ToLower(name.Value()) }

// lessPlatformName orders platforms alphabetically regardless of case.
func lessPlatformName(a, b model.PlatformName) bool {
	if ka, kb := platformKey(a), platformKey(b); ka != kb {
		return ka < kb
	}
	return a.Value() < b.Value()
}

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

// legacyPlatformTypes maps platform keys to the types earlier versions stored in the platforms collection,
// when users could still pick one (Broker, Wallet...). Nothing writes there anymore: any other platform is
// plain Other. Unusable entries are skipped rather than failing the listing.
func legacyPlatformTypes(ctx context.Context, platforms *mongo.Collection, userId model.UserId) (map[string]model.PlatformType, error) {
	cursor, err := platforms.Find(ctx,
		bson.M{"user_id": userId.UUID().String()},
		options.Find().SetProjection(bson.M{"lower_name": 1, "type": 1}),
	)
	if err != nil {
		return nil, err
	}
	var docs []bson.M
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	types := make(map[string]model.PlatformType, len(docs))
	for _, doc := range docs {
		key, _ := doc["lower_name"].(string)
		raw, _ := doc["type"].(string)
		if pt, err := model.NewPlatformType(raw); err == nil && key != "" {
			types[key] = pt
		}
	}
	return types, nil
}

func typeOf(types map[string]model.PlatformType, key string) model.PlatformType {
	if pt, ok := types[key]; ok {
		return pt
	}
	return model.PlatformTypeOther
}
