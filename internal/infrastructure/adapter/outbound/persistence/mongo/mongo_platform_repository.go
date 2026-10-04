package mongo

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// platformGracePeriod keeps DeleteUnused off a platform that a create used this recently: the create may
// have ensured it without its holding having landed yet. Far longer than a create takes (each Mongo
// operation is capped at operationTimeout) and than any clock skew between instances.
const platformGracePeriod = time.Minute

// platformDoc registers the user's spelling of a platform. A holding names its platform with that spelling;
// the doc adds the type and when it was first used.
type platformDoc struct {
	ID         string    `bson:"_id"`
	UserID     string    `bson:"user_id"`
	Name       string    `bson:"name"`
	LowerName  string    `bson:"lower_name"`
	Type       string    `bson:"type"`
	CreatedAt  time.Time `bson:"created_at"`
	LastUsedAt time.Time `bson:"last_used_at"` // absent on docs written before it existed
}

type MongoPlatformRepository struct {
	platformsColl *mongo.Collection
	holdingsColl  *mongo.Collection
}

func NewMongoPlatformRepository(db *MongoDB) *MongoPlatformRepository {
	return &MongoPlatformRepository{
		platformsColl: db.Platforms,
		holdingsColl:  db.Holdings,
	}
}

var _ outbound.PlatformRepository = (*MongoPlatformRepository)(nil)

// FindAll lists the platforms the user's holdings use, sorted by name: a platform lives exactly as long as
// a holding uses it. A doc briefly outliving its last holding (see DeleteUnused) is therefore not listed,
// and a holding whose doc is somehow gone still lists its platform, as type Other.
func (r *MongoPlatformRepository) FindAll(
	ctx context.Context,
	userId model.UserId,
) ([]model.Platform, error) {
	uid := userId.UUID().String()
	cursor, err := r.holdingsColl.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"user_id": uid}}},
		{{Key: "$group", Value: bson.M{"_id": "$platform_name", "first": bson.M{"$min": "$created_at"}}}},
	})
	if err != nil {
		return nil, err
	}
	var used []struct {
		Name  string    `bson:"_id"`
		First time.Time `bson:"first"`
	}
	if err := cursor.All(ctx, &used); err != nil {
		return nil, err
	}

	docs, err := platformDocsByName(ctx, r.platformsColl, uid)
	if err != nil {
		return nil, err
	}

	platforms := make([]model.Platform, 0, len(used))
	for _, u := range used {
		name, err := model.NewPlatformName(u.Name)
		if err != nil {
			continue
		}
		p := model.Platform{UserId: userId, Name: name, Type: model.PlatformTypeOther, CreatedAt: u.First}
		if doc, ok := docs[u.Name]; ok {
			p.Type = platformType(doc)
			p.CreatedAt = doc.CreatedAt
		}
		platforms = append(platforms, p)
	}
	sort.Slice(platforms, func(i, j int) bool { return platforms[i].Name.Value() < platforms[j].Name.Value() })
	return platforms, nil
}

// EnsureExists returns the user's spelling of the platform (matched case-insensitively), registering it if
// it's new, in one atomic upsert: concurrent creates of the same platform get the same doc. Every call also
// stamps last_used_at, which keeps DeleteUnused off it while the caller's holding is on its way.
func (r *MongoPlatformRepository) EnsureExists(
	ctx context.Context,
	userId model.UserId,
	name model.PlatformName,
	now time.Time,
) (model.PlatformName, error) {
	var doc platformDoc
	err := r.platformsColl.FindOneAndUpdate(ctx,
		bson.M{"user_id": userId.UUID().String(), "lower_name": strings.ToLower(name.Value())},
		bson.M{
			"$set": bson.M{"last_used_at": now},
			"$setOnInsert": bson.M{
				"_id":        uuid.NewString(),
				"name":       name.Value(),
				"type":       model.PlatformTypeOther.Value(),
				"created_at": now,
			},
		},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&doc)
	if err != nil {
		return model.PlatformName{}, err
	}
	return model.NewPlatformName(doc.Name)
}

// DeleteUnused removes the user's platform docs that no holding uses anymore, sparing any a create used in
// the last platformGracePeriod: deleting the doc under a create whose holding hasn't landed yet would leave
// that holding without its platform.
func (r *MongoPlatformRepository) DeleteUnused(ctx context.Context, userId model.UserId, now time.Time) error {
	uid := userId.UUID().String()
	inUse := []string{}
	if err := r.holdingsColl.Distinct(ctx, "platform_name", bson.M{"user_id": uid}).Decode(&inUse); err != nil {
		return err
	}
	_, err := r.platformsColl.DeleteMany(ctx, bson.M{
		"user_id": uid,
		"name":    bson.M{"$nin": inUse},
		"$or": bson.A{
			bson.M{"last_used_at": bson.M{"$lt": now.Add(-platformGracePeriod)}},
			bson.M{"last_used_at": bson.M{"$exists": false}},
		},
	})
	return err
}

func platformDocsByName(ctx context.Context, coll *mongo.Collection, uid string) (map[string]platformDoc, error) {
	cursor, err := coll.Find(ctx, bson.M{"user_id": uid})
	if err != nil {
		return nil, err
	}
	var docs []platformDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	byName := make(map[string]platformDoc, len(docs))
	for _, doc := range docs {
		byName[doc.Name] = doc
	}
	return byName, nil
}

// platformType is the doc's type, or Other when it doesn't hold a valid one.
func platformType(doc platformDoc) model.PlatformType {
	pt, err := model.NewPlatformType(doc.Type)
	if err != nil {
		return model.PlatformTypeOther
	}
	return pt
}
