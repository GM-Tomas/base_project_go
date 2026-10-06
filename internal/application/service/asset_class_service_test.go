package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// classesFixture is the asset class service over in-memory holdings and settings, in fake transactions.
type classesFixture struct {
	holdings *mockHoldingRepo
	settings *mockClassSettingsRepo
	quotas   *mockQuotaRepo
	tx       *fakeTx
	svc      *service.AssetClassService
	user     model.UserId
	now      time.Time
}

func newClassesFixture() *classesFixture {
	f := &classesFixture{
		holdings: newMockHoldingRepo(), settings: newMockClassSettingsRepo(), quotas: newMockQuotaRepo(),
		user: model.NewUserId(uuid.New()), now: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC),
	}
	f.tx = &fakeTx{holdings: f.holdings, debts: newMockDebtRepo(), movements: &mockMovementRepo{}, quotas: f.quotas, classes: f.settings}
	f.svc = service.NewAssetClassService(f.tx, f.holdings, f.settings, f.quotas, &holdingsAggregation{holdings: f.holdings},
		service.NewClassDefaults(nil, nil), fixedClock(f.now))
	return f
}

// hold adds a holding of the class worth value, created minutes after the first.
func (f *classesFixture) hold(class string, value float64) model.Holding {
	h := model.Holding{
		Id: model.NewHoldingId(), UserId: f.user, Name: class + " holding", AssetClass: model.MustAssetClass(class),
		Platform: model.MustPlatformName("Broker"), Value: model.MustMoneyFromFloat(value),
		CreatedAt: f.now.Add(time.Duration(len(f.holdings.holdings)) * time.Minute),
	}
	f.holdings.holdings[h.Id.String()] = h
	return h
}

func (f *classesFixture) list(t *testing.T) inbound.AvailableAssetClasses {
	t.Helper()
	res, err := f.svc.GetAvailableAssetClasses(context.Background(), f.user)
	require.NoError(t, err)
	return res
}

func (f *classesFixture) view(t *testing.T, name string) inbound.AssetClassView {
	t.Helper()
	for _, c := range f.list(t).Classes {
		if c.Name.Value() == name {
			return c
		}
	}
	t.Fatalf("no class %q", name)
	return inbound.AssetClassView{}
}

func (f *classesFixture) quota() int {
	return f.quotas.counts[f.user.String()+"/asset_class_settings"]
}

func classId(class string) string {
	return model.AssetClassId(model.MustAssetClass(class))
}

func conflictSlug(err error) string {
	var conflict appErrors.ConflictError
	if errors.As(err, &conflict) {
		return conflict.Slug
	}
	return ""
}

func TestAssetClassService_ListsDefaultsThenTheRestByName(t *testing.T) {
	f := newClassesFixture()
	f.hold("Crypto", 1000)
	f.hold("real estate", 50000)
	f.hold("Art", 200)
	f.hold("Crypto", 500)

	res := f.list(t)
	assert.Equal(t, service.DefaultAssetClasses, res.Defaults)
	assert.Equal(t, []string{"Crypto", "real estate", "Art"}, res.InUse)
	assert.Equal(t, []string{"Cash", "Fixed Income", "Index Fund", "Equity", "Crypto", "Art", "real estate"}, res.All)

	require.Len(t, res.Classes, 7)
	crypto := res.Classes[4]
	assert.Equal(t, "Crypto", crypto.Name.Value())
	assert.True(t, crypto.IsDefault)
	assert.True(t, crypto.Liquid)
	assert.Equal(t, 2, crypto.HoldingsCount)
	assert.Equal(t, "1500.00", crypto.Value.String())
	assert.Nil(t, crypto.Color)
	assert.Nil(t, crypto.ExpectedReturnPct)
	art := res.Classes[5]
	assert.False(t, art.IsDefault)
	assert.False(t, art.Liquid, "a class of the user's is illiquid until they say otherwise")
	fixed := res.Classes[1]
	assert.False(t, fixed.Liquid)
	assert.Equal(t, 0, fixed.HoldingsCount)
	assert.Equal(t, "0.00", fixed.Value.String())
}

func TestAssetClassService_CreateShowsAClassWithoutHoldings(t *testing.T) {
	f := newClassesFixture()
	ctx := context.Background()

	created, err := f.svc.CreateAssetClass(ctx, inbound.CreateAssetClassCommand{
		UserId: f.user, Name: "  Real   Estate ", Color: ptr("#A1B2C3"), Liquid: ptr(false), ExpectedReturnPct: ptr(6.5),
	})
	require.NoError(t, err)
	assert.Equal(t, "Real Estate", created.Name.Value())
	assert.Equal(t, "#a1b2c3", created.Color.Value())
	assert.False(t, created.Liquid)
	assert.Equal(t, "6.5", created.ExpectedReturnPct.String())
	assert.Equal(t, 0, created.HoldingsCount)
	assert.Contains(t, f.list(t).All, "Real Estate")
	assert.Equal(t, 1, f.quota())

	// Without settings it's kept too: the document is what makes it exist.
	_, err = f.svc.CreateAssetClass(ctx, inbound.CreateAssetClassCommand{UserId: f.user, Name: "Art"})
	require.NoError(t, err)
	assert.Equal(t, []string{"Cash", "Fixed Income", "Index Fund", "Equity", "Crypto", "Art", "Real Estate"}, f.list(t).All)
	assert.Equal(t, 2, f.quota())
}

func TestAssetClassService_CreateRefusesOneTheUserHas(t *testing.T) {
	f := newClassesFixture()
	f.hold("Art", 10)
	ctx := context.Background()

	for _, name := range []string{"Cash", "Art", " Art "} {
		_, err := f.svc.CreateAssetClass(ctx, inbound.CreateAssetClassCommand{UserId: f.user, Name: name})
		assert.Equal(t, "class-exists", conflictSlug(err), name)
	}
	// Classes tell case apart, as they always have.
	_, err := f.svc.CreateAssetClass(ctx, inbound.CreateAssetClassCommand{UserId: f.user, Name: "art"})
	assert.NoError(t, err)
}

func TestAssetClassService_CreateChecksWhatsSent(t *testing.T) {
	f := newClassesFixture()
	ctx := context.Background()
	_, err := f.svc.CreateAssetClass(ctx, inbound.CreateAssetClassCommand{UserId: f.user, Name: "  "})
	assert.ErrorIs(t, err, model.ErrBlankLabel)
	_, err = f.svc.CreateAssetClass(ctx, inbound.CreateAssetClassCommand{UserId: f.user, Name: "Art", Color: ptr("red")})
	assert.ErrorIs(t, err, model.ErrInvalidColor)
	_, err = f.svc.CreateAssetClass(ctx, inbound.CreateAssetClassCommand{UserId: f.user, Name: "Art", ExpectedReturnPct: ptr(101.0)})
	assert.ErrorIs(t, err, model.ErrExpectedReturnOutOfRange)
	assert.Empty(t, f.settings.settings)
}

func TestAssetClassService_CapsTheClassesAUserSetsUp(t *testing.T) {
	f := newClassesFixture()
	f.quotas.counts[f.user.String()+"/asset_class_settings"] = model.MaxClassSettingsPerUser
	_, err := f.svc.CreateAssetClass(context.Background(), inbound.CreateAssetClassCommand{UserId: f.user, Name: "Art"})
	var limit appErrors.LimitExceededError
	require.ErrorAs(t, err, &limit)
	assert.Equal(t, "You can set up to 100 asset classes. Remove one to add another.", limit.Message)
	assert.Empty(t, f.settings.settings)
}

func TestAssetClassService_UpdateSetsAndClearsAClassesSettings(t *testing.T) {
	f := newClassesFixture()
	ctx := context.Background()

	updated, err := f.svc.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{
		UserId: f.user, Id: classId("Equity"), Color: set("#112233"), Liquid: set(false), ExpectedReturnPct: set(8.0),
	})
	require.NoError(t, err)
	assert.Equal(t, "#112233", updated.Color.Value())
	assert.False(t, updated.Liquid)
	assert.Equal(t, "8", updated.ExpectedReturnPct.String())
	assert.Equal(t, 1, f.quota())

	// Sending what's there writes nothing.
	saves := f.settings.saves
	_, err = f.svc.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: f.user, Id: classId("Equity"), Liquid: set(false)})
	require.NoError(t, err)
	assert.Equal(t, saves, f.settings.saves)

	// Null clears; a default class back to its defaults needs no document.
	updated, err = f.svc.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{
		UserId: f.user, Id: classId("Equity"), Color: inbound.Change[string]{Set: true}, Liquid: inbound.Change[bool]{Set: true},
		ExpectedReturnPct: inbound.Change[float64]{Set: true},
	})
	require.NoError(t, err)
	assert.Nil(t, updated.Color)
	assert.True(t, updated.Liquid)
	assert.Nil(t, updated.ExpectedReturnPct)
	assert.Empty(t, f.settings.settings)
	assert.Equal(t, 0, f.quota())
}

func TestAssetClassService_UpdateOfAClassTheUserDoesntHaveIsNotFound(t *testing.T) {
	f := newClassesFixture()
	ctx := context.Background()
	for _, classId := range []string{classId("Art"), "not base64!", model.AssetClassId(model.MustAssetClass("Cash")) + "x"} {
		_, err := f.svc.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: f.user, Id: classId, Color: set("#000000")})
		var notFound appErrors.ResourceNotFoundError
		assert.ErrorAs(t, err, &notFound, classId)
	}
	// Someone else's class is not the user's.
	other := model.NewUserId(uuid.New())
	f.holdings.holdings["x"] = model.Holding{Id: model.NewHoldingId(), UserId: other, AssetClass: model.MustAssetClass("Art"),
		Platform: model.MustPlatformName("B"), Value: model.ZeroMoney}
	_, err := f.svc.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: f.user, Id: classId("Art"), Color: set("#000000")})
	var notFound appErrors.ResourceNotFoundError
	assert.ErrorAs(t, err, &notFound)
}

func TestAssetClassService_RenameMovesItsHoldingsAndSettings(t *testing.T) {
	f := newClassesFixture()
	ctx := context.Background()
	_, err := f.svc.CreateAssetClass(ctx, inbound.CreateAssetClassCommand{UserId: f.user, Name: "Stocks", Color: ptr("#abcdef"), ExpectedReturnPct: ptr(7.0)})
	require.NoError(t, err)
	a, b := f.hold("Stocks", 100), f.hold("Stocks", 50)
	f.hold("Crypto", 10)

	renamed, err := f.svc.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: f.user, Id: classId("Stocks"), Name: ptr("Shares"), Liquid: set(true)})
	require.NoError(t, err)
	assert.Equal(t, "Shares", renamed.Name.Value())
	assert.Equal(t, "#abcdef", renamed.Color.Value())
	assert.Equal(t, "7", renamed.ExpectedReturnPct.String())
	assert.True(t, renamed.Liquid)
	assert.Equal(t, 2, renamed.HoldingsCount)
	assert.Equal(t, "Shares", f.holdings.holdings[a.Id.String()].AssetClass.Value())
	assert.Equal(t, "Shares", f.holdings.holdings[b.Id.String()].AssetClass.Value())
	assert.NotContains(t, f.list(t).All, "Stocks")
	_, kept := f.settings.get(f.user, "Stocks")
	assert.False(t, kept)
	assert.Equal(t, 1, f.quota())
}

func TestAssetClassService_RenamingADefaultLeavesItRemoved(t *testing.T) {
	f := newClassesFixture()
	ctx := context.Background()
	h := f.hold("Equity", 100)

	_, err := f.svc.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: f.user, Id: classId("Equity"), Name: ptr("Stocks")})
	require.NoError(t, err)
	assert.Equal(t, "Stocks", f.holdings.holdings[h.Id.String()].AssetClass.Value())
	all := f.list(t).All
	assert.NotContains(t, all, "Equity")
	assert.Contains(t, all, "Stocks")
	hidden, ok := f.settings.get(f.user, "Equity")
	require.True(t, ok)
	assert.True(t, hidden.Hidden)
	assert.Equal(t, 2, f.quota()) // the note that Equity is gone, and Stocks

	// Creating it again brings it back.
	back, err := f.svc.CreateAssetClass(ctx, inbound.CreateAssetClassCommand{UserId: f.user, Name: "Equity"})
	require.NoError(t, err)
	assert.True(t, back.IsDefault)
	assert.Equal(t, []string{"Cash", "Fixed Income", "Index Fund", "Equity", "Crypto", "Stocks"}, f.list(t).All)
	assert.Equal(t, 1, f.quota())
}

func TestAssetClassService_RenameOntoAClassTheUserHasIsAMerge(t *testing.T) {
	f := newClassesFixture()
	ctx := context.Background()
	_, err := f.svc.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: f.user, Id: classId("Equity"), ExpectedReturnPct: set(9.0)})
	require.NoError(t, err)
	_, err = f.svc.CreateAssetClass(ctx, inbound.CreateAssetClassCommand{UserId: f.user, Name: "Stocks", ExpectedReturnPct: ptr(5.0)})
	require.NoError(t, err)
	a := f.hold("Stocks", 100)
	f.hold("Equity", 40)

	_, err = f.svc.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: f.user, Id: classId("Stocks"), Name: ptr("Equity")})
	assert.Equal(t, "class-exists", conflictSlug(err))
	assert.Equal(t, `There's already a class named "Equity"`, err.Error())
	assert.Equal(t, "Stocks", f.holdings.holdings[a.Id.String()].AssetClass.Value())

	merged, err := f.svc.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{
		UserId: f.user, Id: classId("Stocks"), Name: ptr("Equity"), MergeIfExists: true, Color: set("#ffffff"),
	})
	require.NoError(t, err)
	assert.Equal(t, "Equity", merged.Name.Value())
	assert.Equal(t, 2, merged.HoldingsCount)
	assert.Equal(t, "140.00", merged.Value.String())
	assert.Equal(t, "9", merged.ExpectedReturnPct.String(), "the class merged into keeps its settings")
	assert.Nil(t, merged.Color)
	assert.NotContains(t, f.list(t).All, "Stocks")
	assert.Equal(t, 1, f.quota())
}

func TestAssetClassService_RenameOntoARemovedDefaultBringsItBack(t *testing.T) {
	f := newClassesFixture()
	ctx := context.Background()
	require.NoError(t, f.svc.DeleteAssetClass(ctx, inbound.DeleteAssetClassCommand{UserId: f.user, Id: classId("Cash")}))
	f.hold("Money", 10)

	renamed, err := f.svc.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: f.user, Id: classId("Money"), Name: ptr("Cash")})
	require.NoError(t, err)
	assert.True(t, renamed.IsDefault)
	assert.Equal(t, 1, renamed.HoldingsCount)
	assert.Empty(t, f.settings.settings, "Cash is as by default again")
	assert.Equal(t, 0, f.quota())
}

func TestAssetClassService_DeleteMovesItsHoldings(t *testing.T) {
	f := newClassesFixture()
	ctx := context.Background()
	a := f.hold("Art", 100)

	err := f.svc.DeleteAssetClass(ctx, inbound.DeleteAssetClassCommand{UserId: f.user, Id: classId("Art")})
	assert.Equal(t, "class-in-use", conflictSlug(err))
	assert.Equal(t, "Art still has assets: say which class they move to (moveTo)", err.Error())

	err = f.svc.DeleteAssetClass(ctx, inbound.DeleteAssetClassCommand{UserId: f.user, Id: classId("Art"), MoveTo: ptr("Art")})
	var invalid appErrors.ValidationErrors
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "moveTo must be another class", invalid.Errors[0].Message)
	err = f.svc.DeleteAssetClass(ctx, inbound.DeleteAssetClassCommand{UserId: f.user, Id: classId("Art"), MoveTo: ptr(" ")})
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "moveTo", invalid.Errors[0].Field)

	require.NoError(t, f.svc.DeleteAssetClass(ctx, inbound.DeleteAssetClassCommand{UserId: f.user, Id: classId("Art"), MoveTo: ptr("Equity")}))
	assert.Equal(t, "Equity", f.holdings.holdings[a.Id.String()].AssetClass.Value())
	assert.NotContains(t, f.list(t).All, "Art")
}

func TestAssetClassService_DeleteADefaultKeepsItRemoved(t *testing.T) {
	f := newClassesFixture()
	ctx := context.Background()
	_, err := f.svc.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: f.user, Id: classId("Crypto"), Color: set("#000000")})
	require.NoError(t, err)

	require.NoError(t, f.svc.DeleteAssetClass(ctx, inbound.DeleteAssetClassCommand{UserId: f.user, Id: classId("Crypto"), MoveTo: ptr("Cash")}))
	assert.Equal(t, []string{"Cash", "Fixed Income", "Index Fund", "Equity"}, f.list(t).All)
	hidden, _ := f.settings.get(f.user, "Crypto")
	assert.True(t, hidden.Hidden)
	assert.Nil(t, hidden.Color)
	assert.Equal(t, 1, f.quota())

	// Removing it again: it's not one of the user's classes anymore.
	err = f.svc.DeleteAssetClass(ctx, inbound.DeleteAssetClassCommand{UserId: f.user, Id: classId("Crypto")})
	var notFound appErrors.ResourceNotFoundError
	assert.ErrorAs(t, err, &notFound)

	// A holding of a removed default shows it while it has one.
	f.hold("Crypto", 5)
	assert.Contains(t, f.list(t).All, "Crypto")
}

func TestAssetClassService_DeleteACreatedClassFreesItsPlace(t *testing.T) {
	f := newClassesFixture()
	ctx := context.Background()
	_, err := f.svc.CreateAssetClass(ctx, inbound.CreateAssetClassCommand{UserId: f.user, Name: "Art"})
	require.NoError(t, err)
	require.NoError(t, f.svc.DeleteAssetClass(ctx, inbound.DeleteAssetClassCommand{UserId: f.user, Id: classId("Art")}))
	assert.Empty(t, f.settings.settings)
	assert.Equal(t, 0, f.quota())
}

func TestAssetClassService_AFailedWriteChangesNothing(t *testing.T) {
	f := newClassesFixture()
	h := f.hold("Art", 100)
	f.settings.saveErr = errors.New("boom")
	_, err := f.svc.UpdateAssetClass(context.Background(), inbound.UpdateAssetClassCommand{UserId: f.user, Id: classId("Art"), Name: ptr("Paintings")})
	assert.EqualError(t, err, "boom")
	assert.Equal(t, "Art", f.holdings.holdings[h.Id.String()].AssetClass.Value())
	assert.Equal(t, 0, f.quota())
}

func TestAssetClassService_ReadFailures(t *testing.T) {
	f := newClassesFixture()
	f.settings.findErr = errors.New("down")
	_, err := f.svc.GetAvailableAssetClasses(context.Background(), f.user)
	assert.EqualError(t, err, "down")
	_, err = f.svc.CreateAssetClass(context.Background(), inbound.CreateAssetClassCommand{UserId: f.user, Name: "Art"})
	assert.EqualError(t, err, "down")
}

func TestClassDefaults_SkipNamesTheDomainRefuses(t *testing.T) {
	defaults := service.NewClassDefaults([]string{"Cash", " ", "Cash", "Gold"}, []string{"Gold"})
	assert.Equal(t, []model.AssetClass{model.MustAssetClass("Cash"), model.MustAssetClass("Gold")}, defaults.Names)
	assert.Equal(t, []model.AssetClass{model.MustAssetClass("Gold")}, defaults.Liquid)
}

func TestAssetClassService_WriteFailuresRollBack(t *testing.T) {
	ctx := context.Background()

	f := newClassesFixture()
	f.quotas.reserveErr = assert.AnError
	_, err := f.svc.CreateAssetClass(ctx, inbound.CreateAssetClassCommand{UserId: f.user, Name: "Art"})
	assert.ErrorIs(t, err, assert.AnError)

	for _, rename := range []inbound.UpdateAssetClassCommand{
		{Id: classId("Art"), Name: ptr("Paintings")},
		{Id: classId("Art"), Name: ptr("Cash"), MergeIfExists: true},
	} {
		f = newClassesFixture()
		h := f.hold("Art", 10)
		f.holdings.reassignErr = assert.AnError
		rename.UserId = f.user
		_, err = f.svc.UpdateAssetClass(ctx, rename)
		assert.ErrorIs(t, err, assert.AnError)
		assert.Equal(t, "Art", f.holdings.holdings[h.Id.String()].AssetClass.Value())
		err = f.svc.DeleteAssetClass(ctx, inbound.DeleteAssetClassCommand{UserId: f.user, Id: classId("Art"), MoveTo: ptr("Cash")})
		assert.ErrorIs(t, err, assert.AnError)
	}

	f = newClassesFixture()
	_, err = f.svc.CreateAssetClass(ctx, inbound.CreateAssetClassCommand{UserId: f.user, Name: "Art"})
	require.NoError(t, err)
	f.quotas.releaseErr = assert.AnError
	assert.ErrorIs(t, f.svc.DeleteAssetClass(ctx, inbound.DeleteAssetClassCommand{UserId: f.user, Id: classId("Art")}), assert.AnError)
	_, kept := f.settings.get(f.user, "Art")
	assert.True(t, kept, "rolled back")

	f = newClassesFixture()
	f.settings.findErr = assert.AnError
	_, err = f.svc.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: f.user, Id: classId("Cash"), Color: set("#000000")})
	assert.ErrorIs(t, err, assert.AnError)
	assert.ErrorIs(t, f.svc.DeleteAssetClass(ctx, inbound.DeleteAssetClassCommand{UserId: f.user, Id: classId("Cash")}), assert.AnError)
	_, err = f.svc.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: f.user, Id: classId("Cash"), Name: ptr(" ")})
	assert.ErrorIs(t, err, model.ErrBlankLabel)
	_, err = f.svc.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: f.user, Id: classId("Cash"), Color: set("x")})
	assert.ErrorIs(t, err, model.ErrInvalidColor)
	_, err = f.svc.UpdateAssetClass(ctx, inbound.UpdateAssetClassCommand{UserId: f.user, Id: classId("Cash"), ExpectedReturnPct: set(-101.0)})
	assert.ErrorIs(t, err, model.ErrExpectedReturnOutOfRange)
	err = f.svc.DeleteAssetClass(ctx, inbound.DeleteAssetClassCommand{UserId: f.user, Id: "?"})
	var notFound appErrors.ResourceNotFoundError
	assert.ErrorAs(t, err, &notFound)
}
