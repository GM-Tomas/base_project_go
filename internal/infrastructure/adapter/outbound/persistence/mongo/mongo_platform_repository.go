package mongo

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/text/cases"
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
	groups, types, err := platformsWithTypes(ctx, r.holdingsColl, r.platformsColl, userId, false)
	if err != nil {
		return nil, err
	}
	platforms := make([]model.Platform, 0, len(groups))
	for _, g := range slices.SortedFunc(maps.Values(groups), byName) {
		platforms = append(platforms, model.Platform{
			UserId:    userId,
			Name:      g.name,
			Type:      typeOf(types, g.key),
			CreatedAt: g.firstUsed,
		})
	}
	return platforms, nil
}

// Canonical returns the spelling the user's holdings already use for this platform (as every view spells
// it), or name itself for a new one. (Two creates racing on a new platform in different cases can each
// keep their spelling; every view still shows them as one.)
func (r *MongoPlatformRepository) Canonical(
	ctx context.Context,
	userId model.UserId,
	name model.PlatformName,
) (model.PlatformName, error) {
	cursor, err := r.holdingsColl.Find(ctx,
		bson.M{"user_id": userId.UUID().String()},
		options.Find().SetProjection(bson.M{"platform_name": 1}).SetSort(holdingsOldestFirst),
	)
	if err != nil {
		return model.PlatformName{}, err
	}
	defer cursor.Close(ctx)

	// Oldest first, so the first holding of this platform is the one that spells it (platformSpellings).
	want := platformKey(name)
	for cursor.Next(ctx) {
		var doc holdingDoc
		if err := cursor.Decode(&doc); err != nil {
			return model.PlatformName{}, err
		}
		if stored, ok := storedPlatformName(doc); ok && platformKey(stored) == want {
			return stored, nil
		}
	}
	if err := cursor.Err(); err != nil {
		return model.PlatformName{}, err
	}
	return name, nil
}

// platformGroup is one platform: the holdings whose platform names have the same platformKey. The platform
// list shows every platform holdings name; the totals, like every total, count only readable amounts.
type platformGroup struct {
	key       string
	name      model.PlatformName // as platformSpellings spells it
	sortName  string             // see byName
	firstUsed time.Time
	total     model.Money // see readableValue
	count     int         // holdings with a readable amount
}

// platformKey is what makes two platform names the same platform: Unicode's canonical caseless match
// ("Binance" and "binance", "Straße" and "STRASSE", "Café" typed with a precomposed "é" or with "e" and
// a combining accent).
func platformKey(name model.PlatformName) string {
	folder := folders.Get().(*cases.Caser)
	defer folders.Put(folder)
	return norm.NFD.String(folder.String(norm.NFD.String(name.Value())))
}

// folders reuses case folders, which aren't safe for concurrent use, across the many platformKey calls a
// listing makes.
var folders = sync.Pool{New: func() any {
	folder := cases.Fold()
	return &folder
}}

// holdingsOldestFirst is the order the spelling rule is defined in (see platformSpellings).
var holdingsOldestFirst = bson.D{{Key: "created_at", Value: 1}, {Key: "_id", Value: 1}}

// storedPlatformName reads a holding's platform name like the domain reads it (trimmed, inner whitespace
// collapsed), as GET /holdings does. A name the domain rejects can only have been written outside the
// API, and already makes GET /holdings fail; the platform views leave its holding out so they keep working.
func storedPlatformName(doc holdingDoc) (model.PlatformName, bool) {
	name, err := model.NewPlatformName(doc.PlatformName)
	return name, err == nil
}

// platformSpellings holds the one spelling rule: a platform is spelled as on its earliest holding, by
// (created_at, _id), whatever case later ones use. Feed it holdings in that order.
type platformSpellings map[string]model.PlatformName

// spell returns how the platform named on the next holding is spelled, and its key.
func (s platformSpellings) spell(name model.PlatformName) (model.PlatformName, string) {
	key := platformKey(name)
	if first, ok := s[key]; ok {
		return first, key
	}
	s[key] = name
	return name, key
}

// byName orders platforms as a person reads them: ignoring case and accents, so "Álamo" goes with the
// A's rather than after "Zurich". Keys break ties, so the order is total.
func byName(a, b *platformGroup) int {
	return cmp.Or(strings.Compare(a.sortName, b.sortName), strings.Compare(a.key, b.key))
}

// sortName is how a label sorts as a person reads it: case and accents ignored.
func sortName(label string) string {
	unaccented := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, norm.NFD.String(label))
	folder := folders.Get().(*cases.Caser)
	defer folders.Put(folder)
	return folder.String(norm.NFC.String(unaccented))
}

// platformsWithTypes reads the user's platforms (with their totals if asked) and the types earlier
// versions stored at the same time: two independent queries, so a page load waits for one round trip
// rather than two. The first to fail cancels the other, and its error is the one returned.
func platformsWithTypes(
	ctx context.Context,
	holdings, platforms *mongo.Collection,
	userId model.UserId,
	withTotals bool,
) (map[string]*platformGroup, map[string]model.PlatformType, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	type result struct {
		types map[string]model.PlatformType
		err   error
	}
	legacy := make(chan result, 1)
	go func() {
		types, err := legacyPlatformTypes(ctx, platforms, userId)
		if err != nil {
			cancel(err)
		}
		legacy <- result{types, err}
	}()
	groups, err := platformGroups(ctx, holdings, userId, withTotals)
	if err != nil {
		cancel(err)
	}
	res := <-legacy
	if err != nil || res.err != nil {
		return nil, nil, context.Cause(ctx)
	}
	return groups, res.types, nil
}

// platformGroups groups the user's holdings by platform, keyed by platformKey. Only withTotals are the
// amounts read and totalled (total and count stay zero otherwise).
func platformGroups(
	ctx context.Context,
	holdings *mongo.Collection,
	userId model.UserId,
	withTotals bool,
) (map[string]*platformGroup, error) {
	fields := bson.M{"platform_name": 1, "created_at": 1}
	if withTotals {
		fields["value_usd"] = 1
	}
	cursor, err := holdings.Find(ctx,
		bson.M{"user_id": userId.UUID().String()},
		options.Find().SetProjection(fields).SetSort(holdingsOldestFirst),
	)
	if err != nil {
		return nil, err
	}
	var docs []holdingDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	groups := make(map[string]*platformGroup)
	spellings := platformSpellings{}
	for _, doc := range docs {
		stored, ok := storedPlatformName(doc)
		if !ok {
			continue
		}
		name, key := spellings.spell(stored)
		g, ok := groups[key]
		if !ok {
			g = &platformGroup{key: key, name: name, sortName: sortName(name.Value()), firstUsed: doc.CreatedAt, total: model.ZeroMoney}
			groups[key] = g
		}
		if !withTotals {
			continue
		}
		if value, ok := readableValue(doc); ok {
			g.total = g.total.Plus(value)
			g.count++
		}
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
		options.Find().
			SetProjection(bson.M{"lower_name": 1, "type": 1}).
			SetSort(bson.D{{Key: "lower_name", Value: 1}}), // the same winner every time if two now share a key
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
		lower, _ := doc["lower_name"].(string)
		raw, _ := doc["type"].(string)
		name, err := model.NewPlatformName(lower)
		if err != nil {
			continue
		}
		pt, err := model.NewPlatformType(raw)
		if err != nil {
			continue
		}
		// Stored lowercased; keyed again like holdings are (folding a lowercased name gives the same key).
		key := platformKey(name)
		if _, taken := types[key]; !taken {
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
