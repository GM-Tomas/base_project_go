package mongo

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
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
	groups, types, err := platformsWithTypes(ctx, r.holdingsColl, r.platformsColl, userId)
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

// Canonical returns the spelling the user's holdings already use for this platform (as on the earliest of
// them, like every view), or name itself for a new one. One query: each stored spelling with its earliest
// holding. (Two creates racing on a new platform in different cases can each keep their spelling; every
// view still shows them as one.)
func (r *MongoPlatformRepository) Canonical(
	ctx context.Context,
	userId model.UserId,
	name model.PlatformName,
) (model.PlatformName, error) {
	cursor, err := r.holdingsColl.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"user_id": userId.UUID().String()}}},
		{{Key: "$sort", Value: bson.D{{Key: "created_at", Value: 1}, {Key: "_id", Value: 1}}}},
		{{Key: "$group", Value: bson.M{
			"_id":     "$platform_name",
			"firstAt": bson.M{"$first": "$created_at"},
			"firstId": bson.M{"$first": "$_id"},
		}}},
	})
	if err != nil {
		return model.PlatformName{}, err
	}
	var spellings []struct {
		Name    string    `bson:"_id"`
		FirstAt time.Time `bson:"firstAt"`
		FirstID string    `bson:"firstId"`
	}
	if err := cursor.All(ctx, &spellings); err != nil {
		return model.PlatformName{}, err
	}

	canonical, found := name, false
	var at time.Time
	var id string
	for _, sp := range spellings {
		spelled, key, ok := platformOf(sp.Name)
		if !ok || key != platformKey(name) {
			continue
		}
		if !found || sp.FirstAt.Before(at) || (sp.FirstAt.Equal(at) && sp.FirstID < id) {
			canonical, at, id, found = spelled, sp.FirstAt, sp.FirstID, true
		}
	}
	return canonical, nil
}

// platformGroup is one platform: the holdings whose platform names match case-insensitively.
type platformGroup struct {
	name      model.PlatformName // as spelled on the group's earliest holding
	firstUsed time.Time
	total     model.Money // value of the holdings with a valid amount, like NetWorth
	count     int
}

// platformKey is what makes two platform names the same platform.
func platformKey(name model.PlatformName) string { return strings.ToLower(name.Value()) }

// platformOf reads a stored platform name the way the domain does (trimmed, inner whitespace collapsed),
// with its key. ok is false for a name the domain rejects (only writable outside the API). Every view and
// Canonical key platforms through here, so they always agree.
func platformOf(stored string) (name model.PlatformName, key string, ok bool) {
	name, err := model.NewPlatformName(stored)
	if err != nil {
		return model.PlatformName{}, "", false
	}
	return name, platformKey(name), true
}

// lessPlatformName orders platforms as a person reads them: ignoring case and accents, so "Álamo" sorts
// with the A's rather than after "Zurich".
func lessPlatformName(a, b model.PlatformName) bool {
	if ka, kb := foldedForSorting(a), foldedForSorting(b); ka != kb {
		return ka < kb
	}
	if ka, kb := platformKey(a), platformKey(b); ka != kb {
		return ka < kb
	}
	return a.Value() < b.Value()
}

func foldedForSorting(name model.PlatformName) string {
	stripAccents := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	folded, _, err := transform.String(stripAccents, name.Value())
	if err != nil {
		folded = name.Value()
	}
	return strings.ToLower(folded)
}

// platformsWithTypes reads the user's platforms and the types earlier versions stored at the same time:
// two independent queries, so a page load waits for one round trip rather than two.
func platformsWithTypes(
	ctx context.Context,
	holdings, platforms *mongo.Collection,
	userId model.UserId,
) (map[string]*platformGroup, map[string]model.PlatformType, error) {
	type result struct {
		types map[string]model.PlatformType
		err   error
	}
	legacy := make(chan result, 1)
	go func() {
		types, err := legacyPlatformTypes(ctx, platforms, userId)
		legacy <- result{types, err}
	}()
	groups, err := platformGroups(ctx, holdings, userId)
	res := <-legacy
	if err != nil {
		return nil, nil, err
	}
	if res.err != nil {
		return nil, nil, res.err
	}
	return groups, res.types, nil
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
		name, key, ok := platformOf(doc.PlatformName)
		if !ok {
			continue
		}
		g, ok := groups[key]
		if !ok {
			g = &platformGroup{name: name, firstUsed: doc.CreatedAt, total: model.ZeroMoney}
			groups[key] = g
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
// when users could still pick one (Broker, Wallet...). Nothing writes there anymore: the type stays with
// that name (also if the platform is used again after its last holding went), any other one is plain
// Other. Unusable entries are skipped rather than failing the listing.
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
