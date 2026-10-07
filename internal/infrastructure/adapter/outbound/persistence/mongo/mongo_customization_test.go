package mongo

import (
	"context"
	"errors"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func ptrOf[T any](v T) *T { return &v }

func TestAssetClassSettingsRepository(t *testing.T) {
	db := testDB(t)
	repo := NewMongoAssetClassSettingsRepository(db)
	ctx := context.Background()
	user, other := newUser(), newUser()

	art, blue, six := model.MustAssetClass("Art"), model.MustColor("#0000ff"), decimal.RequireFromString("6.5")
	saved := model.AssetClassSettings{UserId: user, Name: art, Color: &blue, Liquid: ptrOf(false), ExpectedReturnPct: &six,
		CreatedAt: at(1), UpdatedAt: at(2)}
	require.NoError(t, repo.Save(ctx, saved))
	require.NoError(t, repo.Save(ctx, model.AssetClassSettings{UserId: user, Name: model.MustAssetClass("Cash"), Hidden: true, CreatedAt: at(1), UpdatedAt: at(1)}))
	require.NoError(t, repo.Save(ctx, model.AssetClassSettings{UserId: other, Name: art, CreatedAt: at(1), UpdatedAt: at(1)}))

	got, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.True(t, got[0].Hidden) // by _id: Cash's id (Q2Fza...) sorts before Art's (QXJ0)
	assert.Equal(t, "Cash", got[0].Name.Value())
	assert.Nil(t, got[0].Liquid)
	assert.Equal(t, saved, got[1])

	// Saving again replaces; nothing set is stored as nothing.
	saved.Color, saved.Liquid, saved.ExpectedReturnPct = nil, nil, nil
	require.NoError(t, repo.Save(ctx, saved))
	var raw bson.M
	require.NoError(t, db.AssetClassSettings.FindOne(ctx, bson.M{"user_id": user.String(), "name": "Art"}).Decode(&raw))
	assert.NotContains(t, raw, "color")
	assert.NotContains(t, raw, "liquid")
	assert.NotContains(t, raw, "expected_return_pct")
	assert.Equal(t, user.String()+"/QXJ0", raw["_id"])

	deleted, err := repo.Delete(ctx, user, art)
	require.NoError(t, err)
	assert.True(t, deleted)
	deleted, err = repo.Delete(ctx, user, art)
	require.NoError(t, err)
	assert.False(t, deleted)
	theirs, err := repo.FindAll(ctx, other)
	require.NoError(t, err)
	assert.Len(t, theirs, 1, "the other user's are theirs")

	// What can't be read (only writable outside the API) is left out, or unset.
	insertRaw(t, db, "asset_class_settings", bson.M{"_id": user.String() + "/x", "user_id": user.String(), "name": " ", "hidden": false})
	insertRaw(t, db, "asset_class_settings", bson.M{"_id": user.String() + "/y", "user_id": user.String(), "name": "Gold",
		"color": "gold", "expected_return_pct": "lots", "hidden": false})
	got, err = repo.FindAll(ctx, user)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "Gold", got[1].Name.Value())
	assert.Nil(t, got[1].Color)
	assert.Nil(t, got[1].ExpectedReturnPct)

	_, err = repo.FindAll(cancelled(), user)
	assert.Error(t, err)
	assert.Error(t, repo.Save(cancelled(), saved))
	_, err = repo.Delete(cancelled(), user, art)
	assert.Error(t, err)
}

func TestPlatformSettingsRepository(t *testing.T) {
	db := testDB(t)
	repo := NewMongoPlatformSettingsRepository(db)
	ctx := context.Background()
	user, other := newUser(), newUser()

	exchange, red := model.MustPlatformType("Exchange"), model.MustColor("#ff0000")
	black := model.MustColor("#000000")
	saved := model.PlatformSettings{UserId: user, Key: "binance", Type: &exchange, AvatarText: ptrOf("🟡"), Color: &red, TextColor: &black, UpdatedAt: at(2)}
	require.NoError(t, repo.Save(ctx, saved))
	require.NoError(t, repo.Save(ctx, model.PlatformSettings{UserId: other, Key: "binance", AvatarText: ptrOf("B"), UpdatedAt: at(2)}))

	got, err := repo.Find(ctx, user, "binance")
	require.NoError(t, err)
	assert.Equal(t, saved, *got)
	missing, err := repo.Find(ctx, user, "nexo")
	require.NoError(t, err)
	assert.Nil(t, missing)
	all, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, []model.PlatformSettings{saved}, all)

	deleted, err := repo.Delete(ctx, user, "binance")
	require.NoError(t, err)
	assert.True(t, deleted)
	deleted, err = repo.Delete(ctx, user, "binance")
	require.NoError(t, err)
	assert.False(t, deleted)
	theirs, _ := repo.Find(ctx, other, "binance")
	require.NotNil(t, theirs)

	insertRaw(t, db, "platform_settings", bson.M{"_id": user.String() + "/x", "user_id": user.String(), "key": ""})
	insertRaw(t, db, "platform_settings", bson.M{"_id": user.String() + "/y", "user_id": user.String(), "key": "nexo",
		"type": "a very long type that is certainly over forty characters", "avatar_text": "ABC", "color": "red", "text_color": "black"})
	all, err = repo.FindAll(ctx, user)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, model.PlatformSettings{UserId: user, Key: "nexo"}, all[0])

	_, err = repo.FindAll(cancelled(), user)
	assert.Error(t, err)
	_, err = repo.Find(cancelled(), user, "nexo")
	assert.Error(t, err)
	assert.Error(t, repo.Save(cancelled(), saved))
	_, err = repo.Delete(cancelled(), user, "nexo")
	assert.Error(t, err)
}

func TestHoldingRepository_Reassigns(t *testing.T) {
	db := testDB(t)
	repo := NewMongoHoldingRepository(db)
	ctx := context.Background()
	user, other := newUser(), newUser()

	a := holding(user, "A", "Crypto", "binance", 100, at(1))
	b := holding(user, "B", "Crypto", "BINANCE", 50, at(2))
	c := holding(user, "C", "Cash", "Bank", 10, at(3))
	theirs := holding(other, "D", "Crypto", "Binance", 1, at(4))
	for _, h := range []model.Holding{a, b, c, theirs} {
		_, err := repo.Save(ctx, h)
		require.NoError(t, err)
	}
	// A class stored in another Unicode form is the same class.
	insertRaw(t, db, "holdings", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "name": "E", "asset_class": " Crypto ",
		"platform_name": "Binance", "value_usd": "1.00", "created_at": at(5), "updated_at": at(5)})

	moved, err := repo.ReassignAssetClass(ctx, user, model.MustAssetClass("Crypto"), model.MustAssetClass("Coins"))
	require.NoError(t, err)
	assert.Equal(t, 3, moved)
	renamed, err := repo.ReassignPlatform(ctx, user, "binance", model.MustPlatformName("Binance"))
	require.NoError(t, err)
	assert.Equal(t, 3, renamed)

	got, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	for _, h := range got {
		if h.Name == "C" {
			assert.Equal(t, "Cash", h.AssetClass.Value())
			assert.Equal(t, "Bank", h.Platform.Value())
			continue
		}
		assert.Equal(t, "Coins", h.AssetClass.Value(), h.Name)
		assert.Equal(t, "Binance", h.Platform.Value(), h.Name)
		assert.Equal(t, h.CreatedAt, h.UpdatedAt, "a rename isn't an edit of the holding")
	}
	mine, _ := repo.FindById(ctx, other, theirs.Id)
	assert.Equal(t, "Crypto", mine.AssetClass.Value(), "another user's holdings stay as they are")

	none, err := repo.ReassignAssetClass(ctx, user, model.MustAssetClass("Gold"), model.MustAssetClass("Coins"))
	require.NoError(t, err)
	assert.Zero(t, none)
	none, err = repo.ReassignPlatform(ctx, user, "nexo", model.MustPlatformName("Nexo"))
	require.NoError(t, err)
	assert.Zero(t, none)
	_, err = repo.ReassignAssetClass(cancelled(), user, model.MustAssetClass("Coins"), model.MustAssetClass("Gold"))
	assert.Error(t, err)
	_, err = repo.ReassignPlatform(cancelled(), user, "binance", model.MustPlatformName("Bnb"))
	assert.Error(t, err)
}

func TestPlatformRepository_TotalsAndNames(t *testing.T) {
	db := testDB(t)
	holdings := NewMongoHoldingRepository(db)
	repo := NewMongoPlatformRepository(db)
	agg := NewMongoWealthAggregationAdapter(db)
	ctx := context.Background()
	user := newUser()
	for _, h := range []model.Holding{
		holding(user, "A", "Crypto", "binance", 100, at(1)),
		holding(user, "B", "Equity", "Binance", 50, at(2)),
		holding(user, "C", "Cash", "Bank", 10, at(3)),
	} {
		_, err := holdings.Save(ctx, h)
		require.NoError(t, err)
	}

	platforms, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	require.Len(t, platforms, 2)
	assert.Equal(t, "Bank", platforms[0].Name.Value())
	assert.Equal(t, "binance", platforms[1].Key)
	assert.Equal(t, 2, platforms[1].Count)
	assert.Equal(t, "150.00", platforms[1].Value.String())

	names, err := repo.Names(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, map[string]model.PlatformName{"bank": model.MustPlatformName("Bank"), "binance": model.MustPlatformName("binance")}, names)
	_, err = repo.Names(cancelled(), user)
	assert.Error(t, err)

	classes, err := agg.ByAssetClass(ctx, user)
	require.NoError(t, err)
	require.Len(t, classes, 3)
	assert.Equal(t, "Crypto", classes[0].AssetClass.Value())
	_, err = agg.ByAssetClass(cancelled(), user)
	assert.Error(t, err)

	totals, err := agg.Totals(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, "Crypto", totals.Returns[0].Class.Value(), "each return comes with its class")
	b := breakdown(t, db, ctx, user)
	assert.Equal(t, "binance", b.ByPlatform[0].Key)
}

// customizationApp is the class and platform services over Mongo, with real transactions.
type customizationApp struct {
	holdings  *MongoHoldingRepository
	classes   *service.AssetClassService
	platforms *service.PlatformService
	summary   *service.WealthQueryService
	quotas    *MongoQuotaRepository
}

func newCustomizationApp(db *MongoDB) customizationApp {
	tx := NewMongoTransactionManager(db)
	holdings := NewMongoHoldingRepository(db)
	quotas := NewMongoQuotaRepository(db)
	classSettings, looks := NewMongoAssetClassSettingsRepository(db), NewMongoPlatformSettingsRepository(db)
	agg := NewMongoWealthAggregationAdapter(db)
	defaults := service.NewClassDefaults(nil, nil)
	return customizationApp{
		holdings:  holdings,
		classes:   service.NewAssetClassService(tx, holdings, classSettings, quotas, agg, defaults, nil),
		platforms: service.NewPlatformService(tx, NewMongoPlatformRepository(db), holdings, looks, quotas, nil),
		summary:   service.NewWealthQueryService(agg, NewMongoSnapshotRepository(db), classSettings, looks, nil, defaults),
		quotas:    quotas,
	}
}

func quotaOf(t *testing.T, db *MongoDB, user model.UserId, key string) int64 {
	t.Helper()
	var counters bson.M
	err := db.Quotas.FindOne(context.Background(), bson.M{"_id": user.String()}).Decode(&counters)
	if err != nil {
		return 0
	}
	return count(counters[key])
}

func TestCustomization_EndToEnd(t *testing.T) {
	db := testDB(t)
	app := newCustomizationApp(db)
	ctx := context.Background()
	user, other := newUser(), newUser()
	for _, h := range []model.Holding{
		holding(user, "AAPL", "Stocks", "IBKR", 600, at(1)),
		holding(user, "MSFT", "Stocks", "ibkr", 400, at(2)),
		holding(user, "BTC", "Crypto", "Binance US", 100, at(3)),
		holding(user, "ETH", "Crypto", "Binance", 50, at(4)),
		holding(other, "Theirs", "Stocks", "IBKR", 1, at(5)),
	} {
		_, err := app.holdings.Save(ctx, h)
		require.NoError(t, err)
	}

	// A class: set up, renamed (with its holdings), merged.
	stocks := model.AssetClassId(model.MustAssetClass("Stocks"))
	view, err := app.classes.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: user, Id: stocks,
		Color: inbound.Change[string]{Set: true, Value: ptrOf("#123456")}, ExpectedReturnPct: inbound.Change[float64]{Set: true, Value: ptrOf(8.0)}})
	require.NoError(t, err)
	assert.Equal(t, 2, view.HoldingsCount)
	assert.Equal(t, "1000.00", view.Value.String())
	view, err = app.classes.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: user, Id: stocks, Name: ptrOf("Shares")})
	require.NoError(t, err)
	assert.Equal(t, "Shares", view.Name.Value())
	assert.Equal(t, "#123456", view.Color.Value())

	summary, err := app.summary.GetSummary(ctx, user)
	require.NoError(t, err)
	// (1000 × 8%) / 1150: Shares' holdings count with its return.
	assert.Equal(t, 6.96, *summary.ExpectedReturn.WeightedPct)
	assert.Equal(t, "#123456", *summary.ByAssetClass[0].Color)

	_, err = app.classes.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: user, Id: model.AssetClassId(model.MustAssetClass("Shares")),
		Name: ptrOf("Crypto")})
	var conflict appErrors.ConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "class-exists", conflict.Slug)
	merged, err := app.classes.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: user, Id: model.AssetClassId(model.MustAssetClass("Shares")),
		Name: ptrOf("Crypto"), MergeIfExists: true})
	require.NoError(t, err)
	assert.Equal(t, 4, merged.HoldingsCount)
	assert.Nil(t, merged.ExpectedReturnPct)

	// A removed default stays removed (and counts: one document).
	require.NoError(t, app.classes.DeleteAssetClass(ctx, inbound.DeleteAssetClassCommand{UserId: user,
		Id: model.AssetClassId(model.MustAssetClass("Crypto")), MoveTo: ptrOf("Equity")}))
	available, err := app.classes.GetAvailableAssetClasses(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, []string{"Cash", "Fixed Income", "Index Fund", "Equity"}, available.All)
	assert.Equal(t, int64(1), quotaOf(t, db, user, "asset_class_settings"))

	// A platform: customized, merged into another (case aside), its look gone with it.
	binanceUS := model.PlatformId(model.PlatformKey(model.MustPlatformName("Binance US")))
	_, err = app.platforms.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: user, Id: binanceUS,
		AvatarText: inbound.Change[string]{Set: true, Value: ptrOf("🇺🇸")}})
	require.NoError(t, err)
	assert.Equal(t, int64(1), quotaOf(t, db, user, "platform_settings"))
	platform, err := app.platforms.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: user, Id: binanceUS, Name: ptrOf("binance"), MergeIfExists: true})
	require.NoError(t, err)
	assert.Equal(t, "Binance", platform.Name.Value())
	assert.Equal(t, 2, platform.Count)
	assert.Nil(t, platform.AvatarText)
	assert.Equal(t, int64(0), quotaOf(t, db, user, "platform_settings"))

	// Respelled on all its holdings.
	ibkr := model.PlatformId(model.PlatformKey(model.MustPlatformName("IBKR")))
	platform, err = app.platforms.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: user, Id: ibkr, Name: ptrOf("Interactive Brokers"),
		Color: inbound.Change[string]{Set: true, Value: ptrOf("#d71920")}})
	require.NoError(t, err)
	assert.Equal(t, "Interactive Brokers", platform.Name.Value())
	assert.Equal(t, "#d71920", platform.Color.Value())

	// None of it touched the other user.
	theirs, err := app.holdings.FindAll(ctx, other)
	require.NoError(t, err)
	assert.Equal(t, "Stocks", theirs[0].AssetClass.Value())
	assert.Equal(t, "IBKR", theirs[0].Platform.Value())
	theirClasses, err := app.classes.GetAvailableAssetClasses(ctx, other)
	require.NoError(t, err)
	assert.Equal(t, []string{"Cash", "Fixed Income", "Index Fund", "Equity", "Crypto", "Stocks"}, theirClasses.All)
	theirPlatforms, err := app.platforms.GetAllPlatforms(ctx, other)
	require.NoError(t, err)
	assert.Nil(t, theirPlatforms[0].Color)
	_, err = app.platforms.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: other,
		Id: model.PlatformId(model.PlatformKey(model.MustPlatformName("Interactive Brokers"))), Name: ptrOf("Mine")})
	var notFound appErrors.ResourceNotFoundError
	assert.ErrorAs(t, err, &notFound)
}

func TestCustomization_AFailedRenameChangesNothing(t *testing.T) {
	db := testDB(t)
	app := newCustomizationApp(db)
	ctx := context.Background()
	user := newUser()
	h := holding(user, "AAPL", "Stocks", "IBKR", 600, at(1))
	_, err := app.holdings.Save(ctx, h)
	require.NoError(t, err)

	// The user is at the cap: the rename would add a document, so it fails after moving the holdings.
	_, err = db.Quotas.UpdateOne(ctx, bson.M{"_id": user.String()}, bson.M{"$set": bson.M{"asset_class_settings": model.MaxClassSettingsPerUser}},
		options.UpdateOne().SetUpsert(true))
	require.NoError(t, err)
	_, err = app.classes.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: user, Id: model.AssetClassId(model.MustAssetClass("Stocks")),
		Name: ptrOf("Shares")})
	var limit appErrors.LimitExceededError
	require.True(t, errors.As(err, &limit), "%v", err)
	got, err := app.holdings.FindById(ctx, user, h.Id)
	require.NoError(t, err)
	assert.Equal(t, "Stocks", got.AssetClass.Value(), "rolled back")
}
