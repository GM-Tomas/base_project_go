package mongo

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Integration tests: they need a real MongoDB. `make test-coverage` starts a throwaway one.
func testDB(t *testing.T) *MongoDB {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("MONGO_TEST_URI not set (make test-coverage starts a throwaway Mongo)")
	}
	ctx := context.Background()
	db, err := NewMongoDB(ctx, uri, "test_"+strings.ReplaceAll(uuid.NewString(), "-", "")[:20])
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Database.Drop(ctx)
		_ = db.Close(ctx)
	})
	return db
}

func cancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// Mongo keeps milliseconds only.
func at(day int) time.Time {
	return time.Date(2026, 1, day, 10, 0, 0, 0, time.UTC)
}

func newUser() model.UserId { return model.NewUserId(uuid.New()) }

func holding(user model.UserId, name, class, platform string, value float64, created time.Time) model.Holding {
	return model.Holding{
		Id:         model.NewHoldingId(),
		UserId:     user,
		Name:       name,
		AssetClass: model.MustAssetClass(class),
		Platform:   model.MustPlatformName(platform),
		Value:      model.MustMoneyFromFloat(value),
		CreatedAt:  created,
		UpdatedAt:  created,
	}
}

// breakdown is Breakdown for tests that look at one of its parts.
func breakdown(t *testing.T, db *MongoDB, ctx context.Context, user model.UserId) outbound.WealthBreakdown {
	t.Helper()
	b, err := NewMongoWealthAggregationAdapter(db).Breakdown(ctx, user)
	require.NoError(t, err)
	return b
}

func insertRaw(t *testing.T, db *MongoDB, coll string, doc bson.M) {
	t.Helper()
	_, err := db.Database.Collection(coll).InsertOne(context.Background(), doc)
	require.NoError(t, err)
}

func TestNewMongoDB_FailsFast(t *testing.T) {
	_, err := NewMongoDB(context.Background(), "not-a-mongo-uri", "x")
	assert.Error(t, err)

	_, err = NewMongoDB(context.Background(), "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=100", "x")
	assert.Error(t, err)
}

func TestNewMongoDB_DefaultsDatabaseName(t *testing.T) {
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("MONGO_TEST_URI not set")
	}
	db, err := NewMongoDB(context.Background(), uri, "")
	require.NoError(t, err)
	defer db.Close(context.Background())

	assert.Equal(t, "base_wealth", db.Database.Name())
}

func TestEnsureIndexes_LogsAndContinuesOnFailure(t *testing.T) {
	db := testDB(t)
	assert.NotPanics(t, func() { db.ensureIndexes(cancelled()) })
}

func TestHoldingRepository(t *testing.T) {
	db := testDB(t)
	repo := NewMongoHoldingRepository(db)
	ctx := context.Background()
	user, other := newUser(), newUser()

	second := holding(user, "ETF", "Index Fund", "IBKR", 200.5, at(2))
	first := holding(user, "Cash", "Cash", "Bank", 100, at(1))
	for _, h := range []model.Holding{second, first, holding(other, "BTC", "Crypto", "Binance", 1, at(1))} {
		_, err := repo.Save(ctx, h)
		require.NoError(t, err)
	}

	got, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, []model.Holding{first, second}, got, "sorted by created_at, scoped to user")

	// Save upserts by id.
	second.Value = model.MustMoneyFromFloat(300)
	_, err = repo.Save(ctx, second)
	require.NoError(t, err)
	got, err = repo.FindAll(ctx, user)
	require.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, "300.00", got[1].Value.String())

	classes, err := repo.AssetClassesInUse(ctx, user)
	require.NoError(t, err)
	assert.ElementsMatch(t, []model.AssetClass{model.MustAssetClass("Cash"), model.MustAssetClass("Index Fund")}, classes)

	deleted, err := repo.DeleteById(ctx, other, first.Id)
	require.NoError(t, err)
	assert.False(t, deleted, "another user's holding is untouched")

	deleted, err = repo.DeleteById(ctx, user, first.Id)
	require.NoError(t, err)
	assert.True(t, deleted)

	got, err = repo.FindAll(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, []model.Holding{second}, got)

	n, err := repo.Count(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	n, err = repo.Count(ctx, other)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestHoldingRepository_SaveNeverOverwritesAnotherUsersHolding(t *testing.T) {
	db := testDB(t)
	repo := NewMongoHoldingRepository(db)
	ctx := context.Background()
	owner, intruder := newUser(), newUser()

	original := holding(owner, "BTC", "Crypto", "Binance", 100, at(1))
	_, err := repo.Save(ctx, original)
	require.NoError(t, err)

	hijack := original
	hijack.UserId = intruder
	hijack.Value = model.MustMoneyFromFloat(1)
	_, err = repo.Save(ctx, hijack)
	assert.ErrorAs(t, err, &appErrors.DuplicateResourceError{}, "same id, different owner: a conflict, never an overwrite")

	got, err := repo.FindAll(ctx, owner)
	require.NoError(t, err)
	assert.Equal(t, []model.Holding{original}, got)
	got, err = repo.FindAll(ctx, intruder)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestHoldingRepository_AssetClassesInUseSkipsInvalid(t *testing.T) {
	db := testDB(t)
	user := newUser()
	insertRaw(t, db, "holdings", bson.M{"_id": "a", "user_id": user.String(), "asset_class": ""})
	insertRaw(t, db, "holdings", bson.M{"_id": "b", "user_id": user.String(), "asset_class": strings.Repeat("x", model.MaxAssetClassLength+1)})
	insertRaw(t, db, "holdings", bson.M{"_id": "c", "user_id": user.String(), "asset_class": "Equity"})

	classes, err := NewMongoHoldingRepository(db).AssetClassesInUse(context.Background(), user)

	require.NoError(t, err)
	assert.Equal(t, []model.AssetClass{model.MustAssetClass("Equity")}, classes)
}

func TestHoldingRepository_ReadsNamesLikeNewHoldingStoresThem(t *testing.T) {
	db := testDB(t)
	user := newUser()
	for name, created := range map[string]time.Time{" Cafe\u0301   bar ": at(1), "": at(2)} {
		insertRaw(t, db, "holdings", bson.M{
			"_id": uuid.NewString(), "user_id": user.String(), "name": name, "asset_class": "Cash",
			"platform_name": "Bank", "value_usd": "1.00", "created_at": created, "updated_at": created,
		})
	}

	list, err := NewMongoHoldingRepository(db).FindAll(context.Background(), user)

	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "Caf\u00e9 bar", list[0].Name)
	assert.Equal(t, "", list[1].Name, "a name the domain rejects is shown as stored")
}

func TestHoldingRepository_AssetClassesInUseListsEachClassOnce(t *testing.T) {
	db := testDB(t)
	user := newUser()
	// Written outside the API or by older versions: spellings the domain reads as one class.
	for id, class := range map[string]string{"a": "Caf\u00e9", "b": "Cafe\u0301", "c": "Equity", "d": " Equity "} {
		insertRaw(t, db, "holdings", bson.M{"_id": id, "user_id": user.String(), "asset_class": class})
	}

	classes, err := NewMongoHoldingRepository(db).AssetClassesInUse(context.Background(), user)

	require.NoError(t, err)
	assert.ElementsMatch(t, []model.AssetClass{model.MustAssetClass("Caf\u00e9"), model.MustAssetClass("Equity")}, classes)
}

func TestPlatformRepository_APlatformWithoutReadableAmountsIsListedButNotBrokenDown(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := newUser()
	_, err := NewMongoHoldingRepository(db).Save(ctx, holding(user, "a", "Cash", "Bank", 5, at(1)))
	require.NoError(t, err)
	// Only writable outside the API.
	insertRaw(t, db, "holdings", bson.M{
		"_id": uuid.NewString(), "user_id": user.String(), "name": "k", "asset_class": "Crypto",
		"platform_name": "Kraken", "value_usd": "abc", "created_at": at(2), "updated_at": at(2),
	})

	// The list shows what holdings name, like the asset classes in use...
	platforms, err := NewMongoPlatformRepository(db).FindAll(ctx, user)
	require.NoError(t, err)
	var listed []string
	for _, p := range platforms {
		listed = append(listed, p.Name.Value())
	}
	assert.Equal(t, []string{"Bank", "Kraken"}, listed)

	// ...the breakdown counts readable amounts, like the class breakdown and net worth.
	byPlatform := breakdown(t, db, ctx, user).ByPlatform
	require.Len(t, byPlatform, 1)
	assert.Equal(t, "Bank", byPlatform[0].Name.Value())
}

func TestWealthAggregation_ByAssetClassBreaksTiesLikeAPerson(t *testing.T) {
	db := testDB(t)
	holdings := NewMongoHoldingRepository(db)
	ctx := context.Background()
	user := newUser()
	for _, h := range []model.Holding{
		holding(user, "a", "Bonds", "Bank", 5, at(1)),
		holding(user, "b", "Ácciones", "Bank", 5, at(1)),
		holding(user, "c", "cash", "Bank", 5, at(1)),
		holding(user, "d", "Cash", "Bank", 5, at(1)),
	} {
		_, err := holdings.Save(ctx, h)
		require.NoError(t, err)
	}

	byClass := breakdown(t, db, ctx, user).ByAssetClass

	var classes []string
	for _, r := range byClass {
		classes = append(classes, r.AssetClass.Value())
	}
	assert.Equal(t, []string{"Ácciones", "Bonds", "Cash", "cash"}, classes, "accents and case ignored, as for platforms")
}

func TestWealthAggregation_ByAssetClassReadsClassesLikeTheDomain(t *testing.T) {
	db := testDB(t)
	user := newUser()
	for i, class := range []string{"Caf\u00e9", "Cafe\u0301"} {
		insertRaw(t, db, "holdings", bson.M{
			"_id": uuid.NewString(), "user_id": user.String(), "name": "x", "asset_class": class,
			"platform_name": "Bank", "value_usd": fmt.Sprintf("%d.00", i+1), "created_at": at(1), "updated_at": at(1),
		})
	}

	byClass := breakdown(t, db, context.Background(), user).ByAssetClass

	require.Len(t, byClass, 1)
	assert.Equal(t, "Caf\u00e9", byClass[0].AssetClass.Value())
	assert.Equal(t, "3.00", byClass[0].Value.String())
	assert.Equal(t, 2, byClass[0].Count)
}

func TestHoldingRepository_Errors(t *testing.T) {
	db := testDB(t)
	repo := NewMongoHoldingRepository(db)
	user := newUser()

	_, err := repo.FindAll(cancelled(), user)
	assert.Error(t, err)
	_, err = repo.Save(cancelled(), holding(user, "x", "Cash", "Bank", 1, at(1)))
	assert.Error(t, err)
	_, err = repo.DeleteById(cancelled(), user, model.NewHoldingId())
	assert.Error(t, err)
	_, err = repo.AssetClassesInUse(cancelled(), user)
	assert.Error(t, err)
	_, err = repo.Count(cancelled(), user)
	assert.Error(t, err)

	// A doc the domain can't accept surfaces as an error, not a zero-value holding.
	insertRaw(t, db, "holdings", bson.M{"_id": "not-a-uuid", "user_id": user.String()})
	_, err = repo.FindAll(context.Background(), user)
	assert.Error(t, err)

	// A doc the driver can't decode.
	bad := newUser()
	insertRaw(t, db, "holdings", bson.M{"_id": uuid.NewString(), "user_id": bad.String(), "value_usd": 5, "asset_class": 5})
	_, err = repo.FindAll(context.Background(), bad)
	assert.Error(t, err)
	_, err = repo.AssetClassesInUse(context.Background(), bad)
	assert.Error(t, err)
}

func TestMapDocToHolding_RejectsCorruptDocs(t *testing.T) {
	valid := holdingDoc{
		ID: uuid.NewString(), UserID: uuid.NewString(), Name: "x",
		AssetClass: "Cash", PlatformName: "Bank", ValueUSD: "1.00",
	}
	_, err := mapDocToHolding(valid)
	require.NoError(t, err)

	for name, corrupt := range map[string]func(*holdingDoc){
		"id":       func(d *holdingDoc) { d.ID = "x" },
		"user id":  func(d *holdingDoc) { d.UserID = "x" },
		"class":    func(d *holdingDoc) { d.AssetClass = "" },
		"platform": func(d *holdingDoc) { d.PlatformName = "" },
		"value":    func(d *holdingDoc) { d.ValueUSD = "abc" },
		"negative": func(d *holdingDoc) { d.ValueUSD = "-1" },
	} {
		t.Run(name, func(t *testing.T) {
			doc := valid
			corrupt(&doc)
			_, err := mapDocToHolding(doc)
			assert.Error(t, err)
		})
	}
}

func TestPlatformRepository(t *testing.T) {
	db := testDB(t)
	repo := NewMongoPlatformRepository(db)
	holdings := NewMongoHoldingRepository(db)
	ctx := context.Background()
	user, other := newUser(), newUser()

	canonical := func(u model.UserId, raw string) string {
		t.Helper()
		name, err := repo.Canonical(ctx, u, model.MustPlatformName(raw))
		require.NoError(t, err)
		return name.Value()
	}
	save := func(h model.Holding) {
		t.Helper()
		_, err := holdings.Save(ctx, h)
		require.NoError(t, err)
	}

	assert.Equal(t, "binance", canonical(user, "binance"), "a new platform keeps the spelling given")

	acc := holding(user, "Account", "Cash", "Abank", 1, at(3))
	save(holding(user, "BTC", "Crypto", "Binance", 1, at(2)))
	save(acc)
	save(holding(other, "ETH", "Crypto", "binance", 1, at(1)))

	assert.Equal(t, "Binance", canonical(user, "BINANCE"), "the spelling the user's holdings use wins")
	assert.Equal(t, "binance", canonical(other, "Binance"), "another user's spelling is their own")

	platforms, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, []model.Platform{
		{UserId: user, Name: model.MustPlatformName("Abank"), Type: model.PlatformTypeOther, CreatedAt: at(3)},
		{UserId: user, Name: model.MustPlatformName("Binance"), Type: model.PlatformTypeOther, CreatedAt: at(2)},
	}, platforms, "sorted by name, created with their first holding, scoped to the user")

	// Gone with its last holding; re-adding it later may then pick a new spelling.
	_, err = holdings.DeleteById(ctx, user, acc.Id)
	require.NoError(t, err)
	platforms, err = repo.FindAll(ctx, user)
	require.NoError(t, err)
	require.Len(t, platforms, 1)
	assert.Equal(t, "Binance", platforms[0].Name.Value())
	assert.Equal(t, "ABANK", canonical(user, "ABANK"))
}

func TestPlatformRepository_CaseVariantsAreOnePlatform(t *testing.T) {
	db := testDB(t)
	repo := NewMongoPlatformRepository(db)
	holdings := NewMongoHoldingRepository(db)
	ctx := context.Background()
	user := newUser()

	// What two creates racing on a new platform in different cases can store.
	for _, h := range []model.Holding{
		holding(user, "a", "Cash", "kraken", 10, at(2)),
		holding(user, "b", "Cash", "Kraken", 5, at(1)),
		holding(user, "c", "Cash", "KRAKEN", 1, at(3)),
	} {
		_, err := holdings.Save(ctx, h)
		require.NoError(t, err)
	}

	platforms, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, []model.Platform{
		{UserId: user, Name: model.MustPlatformName("Kraken"), Type: model.PlatformTypeOther, CreatedAt: at(1)},
	}, platforms, "one platform, spelled as its earliest holding")

	name, err := repo.Canonical(ctx, user, model.MustPlatformName("kRaKeN"))
	require.NoError(t, err)
	assert.Equal(t, "Kraken", name.Value())

	byPlatform := breakdown(t, db, ctx, user).ByPlatform
	require.Len(t, byPlatform, 1)
	assert.Equal(t, "Kraken", byPlatform[0].Name.Value())
	assert.Equal(t, "16.00", byPlatform[0].Value.String())
	assert.Equal(t, 3, byPlatform[0].Count)

	// And every holding comes back under that spelling, so clients can match platforms exactly.
	list, err := holdings.FindAll(ctx, user)
	require.NoError(t, err)
	for _, h := range list {
		assert.Equal(t, "Kraken", h.Platform.Value(), h.Name)
	}
}

func TestPlatformRepository_MatchesNamesUnderFullCaseFolding(t *testing.T) {
	db := testDB(t)
	repo := NewMongoPlatformRepository(db)
	holdings := NewMongoHoldingRepository(db)
	ctx := context.Background()
	user := newUser()

	// Lowercasing alone keeps these apart ("straße" vs "strasse", final sigma "ς" vs "σ", a precomposed
	// "é" vs "e" and a combining accent). The type an earlier version stored under one lowercased spelling
	// applies to the whole platform.
	const cafeComposed, cafeDecomposed = "Caf\u00e9", "Cafe\u0301"
	insertRaw(t, db, "platforms", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "lower_name": "strasse", "type": "Bank"})
	for _, h := range []model.Holding{
		holding(user, "a", "Cash", "Straße", 1, at(1)),
		holding(user, "b", "Cash", "STRASSE", 2, at(2)),
		holding(user, "c", "Cash", "ΟΔΟΣ", 3, at(3)),
		holding(user, "d", "Cash", "οδος", 4, at(4)),
		holding(user, "e", "Cash", cafeComposed, 5, at(5)),
	} {
		_, err := holdings.Save(ctx, h)
		require.NoError(t, err)
	}
	// The API stores labels in NFC; an older version may have stored the decomposed form.
	insertRaw(t, db, "holdings", bson.M{
		"_id": uuid.NewString(), "user_id": user.String(), "name": "f", "asset_class": "Cash",
		"platform_name": cafeDecomposed, "value_usd": "6.00", "created_at": at(6), "updated_at": at(6),
	})

	platforms, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, []model.Platform{
		{UserId: user, Name: model.MustPlatformName(cafeComposed), Type: model.PlatformTypeOther, CreatedAt: at(5)},
		{UserId: user, Name: model.MustPlatformName("Straße"), Type: model.MustPlatformType("Bank"), CreatedAt: at(1)},
		{UserId: user, Name: model.MustPlatformName("ΟΔΟΣ"), Type: model.PlatformTypeOther, CreatedAt: at(3)},
	}, platforms)

	for asked, want := range map[string]string{"strasse": "Straße", "ὁδός": "ὁδός", "οδος": "ΟΔΟΣ", "CAFE\u0301": cafeComposed} {
		name, err := repo.Canonical(ctx, user, model.MustPlatformName(asked))
		require.NoError(t, err)
		assert.Equal(t, want, name.Value(), asked)
	}

	byPlatform := breakdown(t, db, ctx, user).ByPlatform
	var rows []string
	for _, p := range byPlatform {
		rows = append(rows, fmt.Sprintf("%s/%s/%s/%d", p.Name.Value(), p.Type.Value(), p.Value.String(), p.Count))
	}
	assert.Equal(t, []string{cafeComposed + "/Other/11.00/2", "ΟΔΟΣ/Other/7.00/2", "Straße/Bank/3.00/2"}, rows)

	list, err := holdings.FindAll(ctx, user)
	require.NoError(t, err)
	var spelled []string
	for _, h := range list {
		spelled = append(spelled, h.Name+":"+h.Platform.Value())
	}
	assert.Equal(t, []string{"a:Straße", "b:Straße", "c:ΟΔΟΣ", "d:ΟΔΟΣ", "e:" + cafeComposed, "f:" + cafeComposed}, spelled)
}

func TestPlatformRepository_KeepsTypesEarlierVersionsStoredAndSortsAlphabetically(t *testing.T) {
	db := testDB(t)
	repo := NewMongoPlatformRepository(db)
	holdings := NewMongoHoldingRepository(db)
	ctx := context.Background()
	user := newUser()

	// Back when platforms had their own CRUD, users picked a type.
	insertRaw(t, db, "platforms", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "name": "Balanz", "lower_name": "balanz", "type": "Broker"})
	insertRaw(t, db, "platforms", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "lower_name": "nexo", "type": 5})
	insertRaw(t, db, "platforms", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "lower_name": "gone", "type": "Wallet"})
	for _, h := range []model.Holding{
		holding(user, "a", "Equity", "balanz", 30, at(1)),
		holding(user, "b", "Crypto", "Nexo", 20, at(2)),
		holding(user, "c", "Cash", "abank", 10, at(3)),
	} {
		_, err := holdings.Save(ctx, h)
		require.NoError(t, err)
	}

	platforms, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	var listed []string
	for _, p := range platforms {
		listed = append(listed, p.Name.Value()+"/"+p.Type.Value())
	}
	assert.Equal(t, []string{"abank/Other", "balanz/Broker", "Nexo/Other"}, listed,
		"alphabetical regardless of case; a stored type kept, an unusable one Other; no holdings, no platform")

	byPlatform := breakdown(t, db, ctx, user).ByPlatform
	var rows []string
	for _, p := range byPlatform {
		rows = append(rows, p.Name.Value()+"/"+p.Type.Value())
	}
	assert.Equal(t, []string{"balanz/Broker", "Nexo/Other", "abank/Other"}, rows)
}

func TestPlatformRepository_ReadsStoredNamesLikeTheDomainAndSortsLikeAPerson(t *testing.T) {
	db := testDB(t)
	repo := NewMongoPlatformRepository(db)
	holdings := NewMongoHoldingRepository(db)
	ctx := context.Background()
	user := newUser()

	// Written by an older version, with a double space the domain would have collapsed.
	insertRaw(t, db, "holdings", bson.M{
		"_id": uuid.NewString(), "user_id": user.String(), "name": "MP", "asset_class": "Cash",
		"platform_name": "Mercado  Pago", "value_usd": "1.00", "created_at": at(1), "updated_at": at(1),
	})
	name, err := repo.Canonical(ctx, user, model.MustPlatformName("mercado pago"))
	require.NoError(t, err)
	assert.Equal(t, "Mercado Pago", name.Value(), "the same platform as the views show, not a new variant")

	for _, h := range []model.Holding{
		holding(user, "z", "Cash", "Zurich", 1, at(2)),
		holding(user, "a", "Cash", "Álamo", 1, at(3)),
		holding(user, "n", "Cash", "Ñandú", 1, at(4)),
		holding(user, "x", "Cash", "nexo", 1, at(5)),
	} {
		_, err := holdings.Save(ctx, h)
		require.NoError(t, err)
	}
	platforms, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	var names []string
	for _, p := range platforms {
		names = append(names, p.Name.Value())
	}
	assert.Equal(t, []string{"Álamo", "Mercado Pago", "Ñandú", "nexo", "Zurich"}, names, "ignoring case and accents")
}

func TestPlatformRepository_CanonicalAgreesWithTheViewsOnTies(t *testing.T) {
	db := testDB(t)
	repo := NewMongoPlatformRepository(db)
	holdings := NewMongoHoldingRepository(db)
	ctx := context.Background()
	user := newUser()

	for _, spelling := range []string{"kraken", "Kraken", "KRAKEN"} {
		_, err := holdings.Save(ctx, holding(user, spelling, "Cash", spelling, 1, at(1))) // same instant
		require.NoError(t, err)
	}
	platforms, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	require.Len(t, platforms, 1)
	name, err := repo.Canonical(ctx, user, model.MustPlatformName("kraKEN"))
	require.NoError(t, err)
	assert.Equal(t, platforms[0].Name, name)
}

func TestPlatformRepository_Errors(t *testing.T) {
	db := testDB(t)
	repo := NewMongoPlatformRepository(db)
	ctx := context.Background()
	user := newUser()

	_, err := repo.FindAll(cancelled(), user)
	assert.Error(t, err)
	_, err = repo.Canonical(cancelled(), user, model.MustPlatformName("x"))
	assert.Error(t, err)

	// A name the domain rejects (only writable outside the API) is no platform; undecodable data is an error.
	insertRaw(t, db, "holdings", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "platform_name": "", "value_usd": "1"})
	platforms, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	assert.Empty(t, platforms)

	bad := newUser()
	insertRaw(t, db, "holdings", bson.M{"_id": uuid.NewString(), "user_id": bad.String(), "platform_name": 5})
	_, err = repo.FindAll(ctx, bad)
	assert.ErrorContains(t, err, "platform_name")
	assert.NotErrorIs(t, err, context.Canceled, "the error itself, not the cancellation of the other read it caused")
	_, err = repo.Canonical(ctx, bad, model.MustPlatformName("X"))
	assert.Error(t, err)
}

func TestSnapshotRepository(t *testing.T) {
	db := testDB(t)
	repo := NewMongoSnapshotRepository(db)
	ctx := context.Background()
	user, other := newUser(), newUser()

	earliest, err := repo.FindEarliest(ctx, user)
	require.NoError(t, err)
	assert.Nil(t, earliest)

	snap := func(u model.UserId, t time.Time, v float64) model.NetWorthSnapshot {
		return model.NewNetWorthSnapshot(model.NewSnapshotId(), u, t, model.MustMoneyFromFloat(v))
	}
	lastYear := snap(user, time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC), 50)
	jan5 := snap(user, at(5), 200)
	jan2 := snap(user, at(2), 100)
	for _, s := range []model.NetWorthSnapshot{jan5, lastYear, jan2, snap(other, at(1), 1)} {
		_, err := repo.Save(ctx, s)
		require.NoError(t, err)
	}

	all, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, []model.NetWorthSnapshot{lastYear, jan2, jan5}, all)

	exists, err := repo.ExistsAt(ctx, user, at(2))
	require.NoError(t, err)
	assert.True(t, exists)
	exists, err = repo.ExistsAt(ctx, user, at(3))
	require.NoError(t, err)
	assert.False(t, exists)

	first, err := repo.FindFirstOfYear(ctx, user, 2026)
	require.NoError(t, err)
	assert.Equal(t, &jan2, first)

	none, err := repo.FindFirstOfYear(ctx, user, 2027)
	require.NoError(t, err)
	assert.Nil(t, none)

	earliest, err = repo.FindEarliest(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, &lastYear, earliest)

	_, err = repo.Save(ctx, snap(user, at(2), 999))
	assert.ErrorAs(t, err, &appErrors.DuplicateResourceError{})

	// The unique index is per user: another account may snapshot the very same second.
	othersSecond, err := repo.Save(ctx, snap(other, at(2), 7))
	require.NoError(t, err)

	// Deleting is scoped to the owner too.
	deleted, err := repo.DeleteById(ctx, user, othersSecond.Id)
	require.NoError(t, err)
	assert.False(t, deleted, "another user's snapshot is untouched")
	extra, err := repo.Save(ctx, snap(user, at(9), 1))
	require.NoError(t, err)
	deleted, err = repo.DeleteById(ctx, user, extra.Id)
	require.NoError(t, err)
	assert.True(t, deleted)

	n, err := repo.Count(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
	n, err = repo.Count(ctx, other)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
}

func TestSnapshotRepository_Errors(t *testing.T) {
	db := testDB(t)
	repo := NewMongoSnapshotRepository(db)
	user := newUser()

	_, err := repo.FindAll(cancelled(), user)
	assert.Error(t, err)
	_, err = repo.Save(cancelled(), model.NewNetWorthSnapshot(model.NewSnapshotId(), user, at(1), model.ZeroMoney))
	assert.Error(t, err)
	assert.NotErrorIs(t, err, appErrors.DuplicateResourceError{})
	_, err = repo.ExistsAt(cancelled(), user, at(1))
	assert.Error(t, err)
	_, err = repo.FindFirstOfYear(cancelled(), user, 2026)
	assert.Error(t, err)
	_, err = repo.FindEarliest(cancelled(), user)
	assert.Error(t, err)
	_, err = repo.Count(cancelled(), user)
	assert.Error(t, err)
	_, err = repo.DeleteById(cancelled(), user, model.NewSnapshotId())
	assert.Error(t, err)

	insertRaw(t, db, "net_worth_snapshots", bson.M{"_id": "x", "user_id": user.String(), "captured_at": at(1), "total_value_usd": "1"})
	_, err = repo.FindAll(context.Background(), user)
	assert.Error(t, err)
	_, err = repo.FindFirstOfYear(context.Background(), user, 2026)
	assert.Error(t, err)
	_, err = repo.FindEarliest(context.Background(), user)
	assert.Error(t, err)

	bad := newUser()
	insertRaw(t, db, "net_worth_snapshots", bson.M{"_id": uuid.NewString(), "user_id": bad.String(), "captured_at": at(1), "total_value_usd": 5})
	_, err = repo.FindAll(context.Background(), bad)
	assert.Error(t, err)
	_, err = repo.FindFirstOfYear(context.Background(), bad, 2026)
	assert.Error(t, err)
	_, err = repo.FindEarliest(context.Background(), bad)
	assert.Error(t, err)
}

func TestMapDocToSnapshot_RejectsCorruptDocs(t *testing.T) {
	valid := snapshotDoc{ID: uuid.NewString(), UserID: uuid.NewString(), TotalValueUSD: "1.00"}
	_, err := mapDocToSnapshot(valid)
	require.NoError(t, err)

	for name, corrupt := range map[string]func(*snapshotDoc){
		"id":       func(d *snapshotDoc) { d.ID = "x" },
		"user id":  func(d *snapshotDoc) { d.UserID = "x" },
		"value":    func(d *snapshotDoc) { d.TotalValueUSD = "abc" },
		"negative": func(d *snapshotDoc) { d.TotalValueUSD = "-1" },
	} {
		t.Run(name, func(t *testing.T) {
			doc := valid
			corrupt(&doc)
			_, err := mapDocToSnapshot(doc)
			assert.Error(t, err)
		})
	}
}

func TestWealthAggregation(t *testing.T) {
	db := testDB(t)
	agg := NewMongoWealthAggregationAdapter(db)
	holdings := NewMongoHoldingRepository(db)
	ctx := context.Background()
	user := newUser()

	empty, err := agg.NetWorth(ctx, user)
	require.NoError(t, err)
	assert.True(t, empty.IsZero())

	for _, h := range []model.Holding{
		holding(user, "a", "Equity", "IBKR", 100, at(1)),
		holding(user, "b", "Equity", "IBKR", 50, at(1)),
		holding(user, "c", "Cash", "Bank", 150, at(1)),
		holding(user, "d", "Crypto", "Binance", 10, at(1)),
		holding(newUser(), "other", "Cash", "Bank", 1000, at(1)),
	} {
		_, err := holdings.Save(ctx, h)
		require.NoError(t, err)
	}
	// Rows the aggregation must skip rather than fail on.
	insertRaw(t, db, "holdings", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "asset_class": "Cash", "platform_name": "Bank", "value_usd": "abc"})
	insertRaw(t, db, "holdings", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "asset_class": "Cash", "platform_name": "Bank", "value_usd": "-5"})
	insertRaw(t, db, "holdings", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "asset_class": "", "platform_name": "Bank", "value_usd": "1"})

	total, err := agg.NetWorth(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, "311.00", total.String())

	// One read for all three: they agree.
	summary := breakdown(t, db, ctx, user)
	assert.Equal(t, total, summary.NetWorth)
	byClass := summary.ByAssetClass
	type classRow struct {
		Class string
		Value string
		Count int
	}
	var classes []classRow
	for _, r := range byClass {
		classes = append(classes, classRow{r.AssetClass.Value(), r.Value.String(), r.Count})
	}
	assert.Equal(t, []classRow{{"Cash", "150.00", 1}, {"Equity", "150.00", 2}, {"Crypto", "10.00", 1}}, classes,
		"desc by value, ties by name")

	byPlatform := summary.ByPlatform
	type platformRow struct {
		Name  string
		Value string
		Count int
	}
	var rows []platformRow
	for _, r := range byPlatform {
		rows = append(rows, platformRow{r.Name.Value(), r.Value.String(), r.Count})
	}
	assert.Equal(t, []platformRow{
		{"Bank", "151.00", 2},
		{"IBKR", "150.00", 2},
		{"Binance", "10.00", 1},
	}, rows, "desc by value; holdings without a valid amount don't count, like in NetWorth")
}

func TestWealthAggregation_Errors(t *testing.T) {
	db := testDB(t)
	agg := NewMongoWealthAggregationAdapter(db)
	user := newUser()

	_, err := agg.NetWorth(cancelled(), user)
	assert.Error(t, err)
	_, err = agg.Breakdown(cancelled(), user)
	assert.Error(t, err)

	insertRaw(t, db, "holdings", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "value_usd": 5})
	_, err = agg.NetWorth(context.Background(), user)
	assert.Error(t, err)
	_, err = agg.Breakdown(context.Background(), user)
	assert.ErrorContains(t, err, "value_usd", "holdings decode")
	assert.NotErrorIs(t, err, context.Canceled, "the error itself, not the cancellation of the other read it caused")
}

// The summary's holdings come in one reply, with the platform types read alongside: no read per figure,
// no batches of 101.
func TestWealthAggregation_BreakdownReadsHoldingsOnce(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := newUser()
	docs := make([]any, 150)
	for i := range docs {
		docs[i] = bson.M{
			"_id": uuid.NewString(), "user_id": user.String(), "name": "h", "asset_class": "Cash",
			"platform_name": "Bank", "value_usd": "1.00", "created_at": at(1), "updated_at": at(1),
		}
	}
	_, err := db.Holdings.InsertMany(ctx, docs)
	require.NoError(t, err)

	var mu sync.Mutex
	var commands []string
	monitored, err := mongodriver.Connect(options.Client().ApplyURI(os.Getenv("MONGO_TEST_URI")).SetMonitor(&event.CommandMonitor{
		Started: func(_ context.Context, e *event.CommandStartedEvent) {
			mu.Lock()
			defer mu.Unlock()
			if e.CommandName == "find" || e.CommandName == "getMore" || e.CommandName == "aggregate" {
				commands = append(commands, e.CommandName+" "+collectionOf(e))
			}
		},
	}))
	require.NoError(t, err)
	defer func() { _ = monitored.Disconnect(ctx) }()
	database := monitored.Database(db.Database.Name())
	agg := NewMongoWealthAggregationAdapter(&MongoDB{
		Client: monitored, Database: database,
		Holdings: database.Collection("holdings"), Snapshots: database.Collection("net_worth_snapshots"), Platforms: database.Collection("platforms"),
	})

	got, err := agg.Breakdown(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, "150.00", got.NetWorth.String())
	assert.ElementsMatch(t, []string{"find holdings", "find platforms"}, commands)
}

func collectionOf(e *event.CommandStartedEvent) string {
	key := e.CommandName // find names its collection; getMore has it under "collection"
	if key == "getMore" {
		key = "collection"
	}
	if name, ok := e.Command.Lookup(key).StringValueOK(); ok {
		return name
	}
	return "?"
}

// Every holdings read goes oldest first (the spelling rule depends on it). The index must serve that
// order, or Mongo sorts in memory on every listing, platform view and create.
func TestEnsureIndexes_HoldingsOldestFirstNeedsNoSort(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := newUser()
	for i := range 3 {
		_, err := NewMongoHoldingRepository(db).Save(ctx, holding(user, "h", "Cash", "Bank", 1, at(i+1)))
		require.NoError(t, err)
	}

	var explained bson.D
	err := db.Database.RunCommand(ctx, bson.D{
		{Key: "explain", Value: bson.D{
			{Key: "find", Value: db.Holdings.Name()},
			{Key: "filter", Value: bson.M{"user_id": user.String()}},
			{Key: "sort", Value: holdingsOldestFirst},
		}},
		{Key: "verbosity", Value: "queryPlanner"},
	}).Decode(&explained)
	require.NoError(t, err)

	// The stages of the winning plan (not the rejected ones), however the engine nests them.
	var stages []string
	var walk func(any)
	walk = func(node any) {
		switch n := node.(type) {
		case bson.D:
			for _, e := range n {
				if name, ok := e.Value.(string); ok && e.Key == "stage" {
					stages = append(stages, name)
				}
				walk(e.Value)
			}
		case bson.A:
			for _, v := range n {
				walk(v)
			}
		}
	}
	field := func(doc bson.D, key string) any {
		for _, e := range doc {
			if e.Key == key {
				return e.Value
			}
		}
		return nil
	}
	planner, ok := field(explained, "queryPlanner").(bson.D)
	require.True(t, ok, "explain: %v", explained)
	walk(field(planner, "winningPlan"))
	assert.Contains(t, stages, "IXSCAN", "explain: %v", explained)
	assert.NotContains(t, stages, "SORT", "explain: %v", explained)
}

func TestEnsureIndexes_CoversEveryPerUserQuery(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	keysOf := func(coll string) []string {
		cursor, err := db.Database.Collection(coll).Indexes().List(ctx)
		require.NoError(t, err)
		var specs []struct {
			Key    bson.D `bson:"key"`
			Unique bool   `bson:"unique"`
		}
		require.NoError(t, cursor.All(ctx, &specs))
		var out []string
		for _, spec := range specs {
			var fields []string
			for _, k := range spec.Key {
				fields = append(fields, k.Key)
			}
			name := strings.Join(fields, "+")
			if spec.Unique {
				name += " unique"
			}
			out = append(out, name)
		}
		return out
	}

	assert.ElementsMatch(t, []string{"_id", "user_id+created_at+_id", "user_id+asset_class"}, keysOf("holdings"))
	assert.ElementsMatch(t, []string{"_id", "user_id+captured_at unique"}, keysOf("net_worth_snapshots"))
}
