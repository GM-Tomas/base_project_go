package mongo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type platformDoc struct {
	ID        string    `bson:"_id"`
	UserID    string    `bson:"user_id"`
	Name      string    `bson:"name"`
	LowerName string    `bson:"lower_name"`
	Type      string    `bson:"type"`
	CreatedAt time.Time `bson:"created_at"`
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

func (r *MongoPlatformRepository) FindAll(
	ctx context.Context,
	userId model.UserId,
) ([]model.Platform, error) {
	filter := bson.M{"user_id": userId.UUID().String()}
	opts := options.Find().SetSort(bson.D{{Key: "name", Value: 1}})

	cursor, err := r.platformsColl.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []platformDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	platforms := make([]model.Platform, 0, len(docs))
	for _, doc := range docs {
		p, err := mapDocToPlatform(doc)
		if err != nil {
			return nil, err
		}
		platforms = append(platforms, p)
	}

	return platforms, nil
}

func (r *MongoPlatformRepository) findByName(
	ctx context.Context,
	userId model.UserId,
	name model.PlatformName,
) (*model.Platform, error) {
	filter := bson.M{
		"user_id":    userId.UUID().String(),
		"lower_name": strings.ToLower(name.Value()),
	}

	var doc platformDoc
	err := r.platformsColl.FindOne(ctx, filter).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}

	p, err := mapDocToPlatform(doc)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *MongoPlatformRepository) EnsureExists(
	ctx context.Context,
	userId model.UserId,
	name model.PlatformName,
	now time.Time,
) (model.PlatformName, error) {
	existing, err := r.findByName(ctx, userId, name)
	if err != nil {
		return model.PlatformName{}, err
	}
	if existing != nil {
		return existing.Name, nil
	}

	newPlatform := model.Platform{
		UserId:    userId,
		Name:      name,
		Type:      model.PlatformTypeOther,
		CreatedAt: now,
	}

	err = r.insert(ctx, newPlatform)
	if err != nil {
		// If duplicate concurrent insert occurred, return existing
		if mongo.IsDuplicateKeyError(err) {
			found, findErr := r.findByName(ctx, userId, name)
			if findErr == nil && found != nil {
				return found.Name, nil
			}
		}
		return model.PlatformName{}, err
	}

	return name, nil
}

// insert returns the raw driver error so EnsureExists can recognise a concurrent duplicate insert.
func (r *MongoPlatformRepository) insert(ctx context.Context, platform model.Platform) error {
	_, err := r.platformsColl.InsertOne(ctx, platformDoc{
		ID:        uuid.New().String(),
		UserID:    platform.UserId.UUID().String(),
		Name:      platform.Name.Value(),
		LowerName: strings.ToLower(platform.Name.Value()),
		Type:      platform.Type.Value(),
		CreatedAt: platform.CreatedAt,
	})
	return err
}

func (r *MongoPlatformRepository) DeleteUnused(ctx context.Context, userId model.UserId) error {
	uid := userId.UUID().String()
	var inUse []string
	if err := r.holdingsColl.Distinct(ctx, "platform_name", bson.M{"user_id": uid}).Decode(&inUse); err != nil {
		return err
	}
	// Not atomic with a concurrent create on the same platform by the same user (other users' data is
	// never involved): the holding can end up without its platform doc. ByPlatform still counts it.
	_, err := r.platformsColl.DeleteMany(ctx, bson.M{"user_id": uid, "name": bson.M{"$nin": inUse}})
	return err
}

func mapDocToPlatform(doc platformDoc) (model.Platform, error) {
	userId, err := uuid.Parse(doc.UserID)
	if err != nil {
		return model.Platform{}, err
	}

	pn, err := model.NewPlatformName(doc.Name)
	if err != nil {
		return model.Platform{}, err
	}

	pt, err := model.NewPlatformType(doc.Type)
	if err != nil {
		return model.Platform{}, err
	}

	return model.Platform{
		UserId:    model.NewUserId(userId),
		Name:      pn,
		Type:      pt,
		CreatedAt: doc.CreatedAt,
	}, nil
}
