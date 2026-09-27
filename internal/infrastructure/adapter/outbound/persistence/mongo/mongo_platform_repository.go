package mongo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
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

func (r *MongoPlatformRepository) FindByName(
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
	existing, err := r.FindByName(ctx, userId, name)
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

	_, err = r.Save(ctx, newPlatform)
	if err != nil {
		// If duplicate concurrent insert occurred, return existing
		if mongo.IsDuplicateKeyError(err) {
			found, findErr := r.FindByName(ctx, userId, name)
			if findErr == nil && found != nil {
				return found.Name, nil
			}
		}
		return model.PlatformName{}, err
	}

	return name, nil
}

func (r *MongoPlatformRepository) Save(
	ctx context.Context,
	platform model.Platform,
) (model.Platform, error) {
	doc := platformDoc{
		ID:        uuid.New().String(),
		UserID:    platform.UserId.UUID().String(),
		Name:      platform.Name.Value(),
		LowerName: strings.ToLower(platform.Name.Value()),
		Type:      platform.Type.Value(),
		CreatedAt: platform.CreatedAt,
	}

	_, err := r.platformsColl.InsertOne(ctx, doc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return model.Platform{}, appErrors.DuplicateResourceError{
				Message: "Platform '" + platform.Name.Value() + "' already exists",
			}
		}
		return model.Platform{}, err
	}

	return platform, nil
}

func (r *MongoPlatformRepository) Update(
	ctx context.Context,
	userId model.UserId,
	currentName model.PlatformName,
	newName *model.PlatformName,
	newType *model.PlatformType,
) (*model.Platform, error) {
	existing, err := r.FindByName(ctx, userId, currentName)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, nil
	}

	updateFields := bson.M{}
	updatedName := existing.Name
	updatedType := existing.Type

	if newName != nil {
		updateFields["name"] = newName.Value()
		updateFields["lower_name"] = strings.ToLower(newName.Value())
		updatedName = *newName
	}
	if newType != nil {
		updateFields["type"] = newType.Value()
		updatedType = *newType
	}

	if len(updateFields) == 0 {
		return existing, nil
	}

	filter := bson.M{
		"user_id":    userId.UUID().String(),
		"lower_name": strings.ToLower(currentName.Value()),
	}

	_, err = r.platformsColl.UpdateOne(ctx, filter, bson.M{"$set": updateFields})
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, appErrors.DuplicateResourceError{
				Message: "Platform '" + updatedName.Value() + "' already exists",
			}
		}
		return nil, err
	}

	// Cascading update on holdings if platform name changed
	if newName != nil && currentName.Value() != newName.Value() {
		hFilter := bson.M{
			"user_id":       userId.UUID().String(),
			"platform_name": currentName.Value(),
		}
		hUpdate := bson.M{
			"$set": bson.M{
				"platform_name": newName.Value(),
				"updated_at":    time.Now().UTC(),
			},
		}
		_, _ = r.holdingsColl.UpdateMany(ctx, hFilter, hUpdate)
	}

	return &model.Platform{
		UserId:    userId,
		Name:      updatedName,
		Type:      updatedType,
		CreatedAt: existing.CreatedAt,
	}, nil
}

func (r *MongoPlatformRepository) DeleteByName(
	ctx context.Context,
	userId model.UserId,
	name model.PlatformName,
) (bool, error) {
	filter := bson.M{
		"user_id":    userId.UUID().String(),
		"lower_name": strings.ToLower(name.Value()),
	}

	res, err := r.platformsColl.DeleteOne(ctx, filter)
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

func (r *MongoPlatformRepository) CountHoldings(
	ctx context.Context,
	userId model.UserId,
	name model.PlatformName,
) (int, error) {
	filter := bson.M{
		"user_id":       userId.UUID().String(),
		"platform_name": name.Value(),
	}

	count, err := r.holdingsColl.CountDocuments(ctx, filter)
	if err != nil {
		return 0, err
	}
	return int(count), nil
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
