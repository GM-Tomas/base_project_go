package mongo

import (
	"context"
	"errors"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// assetClassSettingsDoc is what a user set for one of their classes. Its _id is the user's id and the
// class's (settingsId), so a user has at most one per class without another unique index.
type assetClassSettingsDoc struct {
	ID     string `bson:"_id"`
	UserID string `bson:"user_id"`
	Name   string `bson:"name"`
	Color  string `bson:"color,omitempty"`
	Liquid *bool  `bson:"liquid,omitempty"`
	// ExpectedReturnPCT is decimal text, as a holding's.
	ExpectedReturnPCT string    `bson:"expected_return_pct,omitempty"`
	Hidden            bool      `bson:"hidden"`
	CreatedAt         time.Time `bson:"created_at"`
	UpdatedAt         time.Time `bson:"updated_at"`
}

// platformSettingsDoc is how a user set up one of their platforms, by its key.
type platformSettingsDoc struct {
	ID         string    `bson:"_id"`
	UserID     string    `bson:"user_id"`
	Key        string    `bson:"key"`
	Type       string    `bson:"type,omitempty"`
	AvatarText string    `bson:"avatar_text,omitempty"`
	Color      string    `bson:"color,omitempty"`
	TextColor  string    `bson:"text_color,omitempty"`
	UpdatedAt  time.Time `bson:"updated_at"`
}

// settingsId is the _id of a user's settings for one class or platform (by the id the API names it with).
func settingsId(userId model.UserId, id string) string {
	return userId.UUID().String() + "/" + id
}

// settingsPerReply fits every document a user can have (see model.MaxPlatformSettingsPerUser) in one reply.
const settingsPerReply = int32(model.MaxPlatformSettingsPerUser + 1)

type MongoAssetClassSettingsRepository struct {
	coll *mongo.Collection
}

func NewMongoAssetClassSettingsRepository(db *MongoDB) *MongoAssetClassSettingsRepository {
	return &MongoAssetClassSettingsRepository{coll: db.AssetClassSettings}
}

var _ outbound.AssetClassSettingsRepository = (*MongoAssetClassSettingsRepository)(nil)

// FindAll reads the user's class settings. One that can't be read (only writable outside the API) is left
// out, and a value that can't is unset, rather than failing every view of the classes.
func (r *MongoAssetClassSettingsRepository) FindAll(ctx context.Context, userId model.UserId) ([]model.AssetClassSettings, error) {
	cursor, err := r.coll.Find(ctx, bson.M{"user_id": userId.UUID().String()},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetBatchSize(settingsPerReply))
	if err != nil {
		return nil, err
	}
	var docs []assetClassSettingsDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	settings := make([]model.AssetClassSettings, 0, len(docs))
	for _, doc := range docs {
		class, err := model.NewAssetClass(doc.Name)
		if err != nil || class.Value() != doc.Name {
			continue
		}
		settings = append(settings, model.AssetClassSettings{
			UserId:            userId,
			Name:              class,
			Color:             readColor(doc.Color),
			Liquid:            doc.Liquid,
			ExpectedReturnPct: readReturn(doc.ExpectedReturnPCT),
			Hidden:            doc.Hidden,
			CreatedAt:         doc.CreatedAt,
			UpdatedAt:         doc.UpdatedAt,
		})
	}
	return settings, nil
}

func (r *MongoAssetClassSettingsRepository) Save(ctx context.Context, s model.AssetClassSettings) error {
	doc := assetClassSettingsDoc{
		ID:                settingsId(s.UserId, model.AssetClassId(s.Name)),
		UserID:            s.UserId.UUID().String(),
		Name:              s.Name.Value(),
		Color:             colorText(s.Color),
		Liquid:            s.Liquid,
		ExpectedReturnPCT: returnText(s.ExpectedReturnPct),
		Hidden:            s.Hidden,
		CreatedAt:         s.CreatedAt,
		UpdatedAt:         s.UpdatedAt,
	}
	_, err := r.coll.ReplaceOne(ctx, bson.M{"_id": doc.ID}, doc, options.Replace().SetUpsert(true))
	return err
}

func (r *MongoAssetClassSettingsRepository) Delete(ctx context.Context, userId model.UserId, class model.AssetClass) (bool, error) {
	res, err := r.coll.DeleteOne(ctx, bson.M{"_id": settingsId(userId, model.AssetClassId(class))})
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

type MongoPlatformSettingsRepository struct {
	coll *mongo.Collection
}

func NewMongoPlatformSettingsRepository(db *MongoDB) *MongoPlatformSettingsRepository {
	return &MongoPlatformSettingsRepository{coll: db.PlatformSettings}
}

var _ outbound.PlatformSettingsRepository = (*MongoPlatformSettingsRepository)(nil)

func (r *MongoPlatformSettingsRepository) FindAll(ctx context.Context, userId model.UserId) ([]model.PlatformSettings, error) {
	cursor, err := r.coll.Find(ctx, bson.M{"user_id": userId.UUID().String()},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetBatchSize(settingsPerReply))
	if err != nil {
		return nil, err
	}
	var docs []platformSettingsDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	settings := make([]model.PlatformSettings, 0, len(docs))
	for _, doc := range docs {
		if doc.Key == "" {
			continue
		}
		settings = append(settings, readPlatformSettings(userId, doc))
	}
	return settings, nil
}

func (r *MongoPlatformSettingsRepository) Find(ctx context.Context, userId model.UserId, key string) (*model.PlatformSettings, error) {
	var doc platformSettingsDoc
	err := r.coll.FindOne(ctx, bson.M{"_id": settingsId(userId, model.PlatformId(key))}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s := readPlatformSettings(userId, doc)
	return &s, nil
}

func (r *MongoPlatformSettingsRepository) Save(ctx context.Context, s model.PlatformSettings) error {
	doc := platformSettingsDoc{
		ID:        settingsId(s.UserId, model.PlatformId(s.Key)),
		UserID:    s.UserId.UUID().String(),
		Key:       s.Key,
		Color:     colorText(s.Color),
		TextColor: colorText(s.TextColor),
		UpdatedAt: s.UpdatedAt,
	}
	if s.Type != nil {
		doc.Type = s.Type.Value()
	}
	if s.AvatarText != nil {
		doc.AvatarText = *s.AvatarText
	}
	_, err := r.coll.ReplaceOne(ctx, bson.M{"_id": doc.ID}, doc, options.Replace().SetUpsert(true))
	return err
}

func (r *MongoPlatformSettingsRepository) Delete(ctx context.Context, userId model.UserId, key string) (bool, error) {
	res, err := r.coll.DeleteOne(ctx, bson.M{"_id": settingsId(userId, model.PlatformId(key))})
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

// readPlatformSettings reads them as stored, a value the API wouldn't take being unset.
func readPlatformSettings(userId model.UserId, doc platformSettingsDoc) model.PlatformSettings {
	s := model.PlatformSettings{UserId: userId, Key: doc.Key, Color: readColor(doc.Color), TextColor: readColor(doc.TextColor), UpdatedAt: doc.UpdatedAt}
	if doc.Type != "" {
		if pt, err := model.NewPlatformType(doc.Type); err == nil {
			s.Type = &pt
		}
	}
	if doc.AvatarText != "" {
		if text, err := model.NewAvatarText(doc.AvatarText); err == nil {
			s.AvatarText = &text
		}
	}
	return s
}

func readColor(s string) *model.Color {
	if s == "" {
		return nil
	}
	c, err := model.NewColor(s)
	if err != nil {
		return nil
	}
	return &c
}

func colorText(c *model.Color) string {
	if c == nil {
		return ""
	}
	return c.Value()
}
