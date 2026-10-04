package mongo

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
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

	name, err := repo.EnsureExists(ctx, user, model.MustPlatformName("Binance"), at(1))
	require.NoError(t, err)
	assert.Equal(t, "Binance", name.Value())

	// Case-insensitive match returns the stored spelling instead of creating a duplicate.
	name, err = repo.EnsureExists(ctx, user, model.MustPlatformName("binance"), at(2))
	require.NoError(t, err)
	assert.Equal(t, "Binance", name.Value())

	_, err = repo.EnsureExists(ctx, user, model.MustPlatformName("Abank"), at(3))
	require.NoError(t, err)
	_, err = repo.EnsureExists(ctx, other, model.MustPlatformName("Binance"), at(1))
	require.NoError(t, err)

	platforms, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, []model.Platform{
		{UserId: user, Name: model.MustPlatformName("Abank"), Type: model.PlatformTypeOther, CreatedAt: at(3)},
		{UserId: user, Name: model.MustPlatformName("Binance"), Type: model.PlatformTypeOther, CreatedAt: at(1)},
	}, platforms, "sorted by name, scoped to user")

	_, err = holdings.Save(ctx, holding(user, "BTC", "Crypto", "Binance", 1, at(1)))
	require.NoError(t, err)
	require.NoError(t, repo.DeleteUnused(ctx, user))

	platforms, err = repo.FindAll(ctx, user)
	require.NoError(t, err)
	require.Len(t, platforms, 1)
	assert.Equal(t, "Binance", platforms[0].Name.Value())

	otherPlatforms, err := repo.FindAll(ctx, other)
	require.NoError(t, err)
	assert.Len(t, otherPlatforms, 1, "DeleteUnused is scoped to the user")
}

func TestPlatformRepository_UniqueIndex(t *testing.T) {
	db := testDB(t)
	repo := NewMongoPlatformRepository(db)
	user := newUser()
	p := model.NewPlatform(user, model.MustPlatformName("Binance"), model.PlatformTypeOther, at(1))

	require.NoError(t, repo.insert(context.Background(), p))
	assert.Error(t, repo.insert(context.Background(), p))
}

func TestPlatformRepository_Errors(t *testing.T) {
	db := testDB(t)
	repo := NewMongoPlatformRepository(db)
	user := newUser()

	_, err := repo.FindAll(cancelled(), user)
	assert.Error(t, err)
	_, err = repo.EnsureExists(cancelled(), user, model.MustPlatformName("x"), at(1))
	assert.Error(t, err)
	assert.Error(t, repo.DeleteUnused(cancelled(), user))

	insertRaw(t, db, "platforms", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "name": "", "lower_name": "broken"})
	_, err = repo.FindAll(context.Background(), user)
	assert.Error(t, err, "invalid stored name")
	_, err = repo.EnsureExists(context.Background(), user, model.MustPlatformName("Broken"), at(1))
	assert.Error(t, err)

	bad := newUser()
	insertRaw(t, db, "platforms", bson.M{"_id": uuid.NewString(), "user_id": bad.String(), "lower_name": "x", "created_at": "yesterday"})
	insertRaw(t, db, "holdings", bson.M{"_id": uuid.NewString(), "user_id": bad.String(), "platform_name": 5})
	_, err = repo.FindAll(context.Background(), bad)
	assert.Error(t, err)
	_, err = repo.EnsureExists(context.Background(), bad, model.MustPlatformName("X"), at(1))
	assert.Error(t, err)
	assert.Error(t, repo.DeleteUnused(context.Background(), bad))
}

func TestMapDocToPlatform_RejectsCorruptDocs(t *testing.T) {
	valid := platformDoc{UserID: uuid.NewString(), Name: "Binance", Type: "Exchange"}
	_, err := mapDocToPlatform(valid)
	require.NoError(t, err)

	for name, corrupt := range map[string]func(*platformDoc){
		"user id": func(d *platformDoc) { d.UserID = "x" },
		"name":    func(d *platformDoc) { d.Name = "" },
		"type":    func(d *platformDoc) { d.Type = strings.Repeat("x", model.MaxPlatformTypeLength+1) },
	} {
		t.Run(name, func(t *testing.T) {
			doc := valid
			corrupt(&doc)
			_, err := mapDocToPlatform(doc)
			assert.Error(t, err)
		})
	}
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
	platforms := NewMongoPlatformRepository(db)
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
	for _, name := range []string{"IBKR", "Bank", "Binance", "Empty"} {
		_, err := platforms.EnsureExists(ctx, user, model.MustPlatformName(name), at(1))
		require.NoError(t, err)
	}
	// Rows the aggregation must skip rather than fail on.
	insertRaw(t, db, "holdings", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "asset_class": "Cash", "platform_name": "Bank", "value_usd": "abc"})
	insertRaw(t, db, "holdings", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "asset_class": "Cash", "platform_name": "Bank", "value_usd": "-5"})
	insertRaw(t, db, "holdings", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "asset_class": "", "platform_name": "Bank", "value_usd": "1"})
	insertRaw(t, db, "platforms", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "name": "", "lower_name": "", "type": "Other"})
	insertRaw(t, db, "platforms", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "name": "BadType", "lower_name": "badtype", "type": strings.Repeat("x", 100)})

	total, err := agg.NetWorth(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, "311.00", total.String())

	byClass, err := agg.ByAssetClass(ctx, user)
	require.NoError(t, err)
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

	byPlatform, err := agg.ByPlatform(ctx, user)
	require.NoError(t, err)
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
		{"Empty", "0.00", 0},
	}, rows, "includes platforms without holdings")
}

func TestWealthAggregation_Errors(t *testing.T) {
	db := testDB(t)
	agg := NewMongoWealthAggregationAdapter(db)
	user := newUser()

	_, err := agg.NetWorth(cancelled(), user)
	assert.Error(t, err)
	_, err = agg.ByAssetClass(cancelled(), user)
	assert.Error(t, err)
	_, err = agg.ByPlatform(cancelled(), user)
	assert.Error(t, err)

	insertRaw(t, db, "holdings", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "value_usd": 5})
	_, err = agg.NetWorth(context.Background(), user)
	assert.Error(t, err)
	_, err = agg.ByAssetClass(context.Background(), user)
	assert.Error(t, err)
	_, err = agg.ByPlatform(context.Background(), user)
	assert.Error(t, err, "holdings decode")

	bad := newUser()
	insertRaw(t, db, "platforms", bson.M{"_id": uuid.NewString(), "user_id": bad.String(), "created_at": "yesterday"})
	_, err = agg.ByPlatform(context.Background(), bad)
	assert.Error(t, err, "platforms decode")
}

func TestWealthAggregation_CountsHoldingsWhosePlatformDocIsGone(t *testing.T) {
	db := testDB(t)
	agg := NewMongoWealthAggregationAdapter(db)
	holdings := NewMongoHoldingRepository(db)
	platforms := NewMongoPlatformRepository(db)
	ctx := context.Background()
	user := newUser()

	_, err := platforms.EnsureExists(ctx, user, model.MustPlatformName("IBKR"), at(1))
	require.NoError(t, err)
	for _, h := range []model.Holding{
		holding(user, "a", "Equity", "IBKR", 100, at(1)),
		// What a create racing a delete's DeleteUnused can leave behind: no "Lost" platform doc.
		holding(user, "b", "Cash", "Lost", 40, at(1)),
	} {
		_, err := holdings.Save(ctx, h)
		require.NoError(t, err)
	}

	byPlatform, err := agg.ByPlatform(ctx, user)
	require.NoError(t, err)
	require.Len(t, byPlatform, 2)
	assert.Equal(t, "Lost", byPlatform[1].Name.Value())
	assert.Equal(t, model.PlatformTypeOther, byPlatform[1].Type)
	assert.Equal(t, "40.00", byPlatform[1].Value.String())
	assert.Equal(t, 1, byPlatform[1].Count)
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

	assert.ElementsMatch(t, []string{"_id", "user_id+created_at", "user_id+platform_name", "user_id+asset_class"}, keysOf("holdings"))
	assert.ElementsMatch(t, []string{"_id", "user_id+lower_name unique"}, keysOf("platforms"))
	assert.ElementsMatch(t, []string{"_id", "user_id+captured_at unique"}, keysOf("net_worth_snapshots"))
}
