package service_test

import (
	"context"
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

// platformsFixture is the platform service over in-memory holdings and settings, in fake transactions.
type platformsFixture struct {
	holdings *mockHoldingRepo
	looks    *mockPlatformSettingsRepo
	quotas   *mockQuotaRepo
	svc      *service.PlatformService
	user     model.UserId
	now      time.Time
}

func newPlatformsFixture() *platformsFixture {
	f := &platformsFixture{
		holdings: newMockHoldingRepo(), looks: newMockPlatformSettingsRepo(), quotas: newMockQuotaRepo(),
		user: model.NewUserId(uuid.New()), now: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC),
	}
	tx := &fakeTx{holdings: f.holdings, debts: newMockDebtRepo(), movements: &mockMovementRepo{}, quotas: f.quotas, looks: f.looks}
	f.svc = service.NewPlatformService(tx, newMockPlatformRepo(f.holdings), f.holdings, f.looks, f.quotas, fixedClock(f.now))
	return f
}

func (f *platformsFixture) hold(platform string, value float64) model.Holding {
	h := model.Holding{
		Id: model.NewHoldingId(), UserId: f.user, Name: "On " + platform, AssetClass: model.MustAssetClass("Cash"),
		Platform: model.MustPlatformName(platform), Value: model.MustMoneyFromFloat(value),
		CreatedAt: f.now.Add(time.Duration(len(f.holdings.holdings)) * time.Minute),
	}
	f.holdings.holdings[h.Id.String()] = h
	return h
}

func platformId(name string) string {
	return model.PlatformId(model.PlatformKey(model.MustPlatformName(name)))
}

func (f *platformsFixture) quota() int {
	return f.quotas.counts[f.user.String()+"/platform_settings"]
}

func TestPlatformService_GetAllPlatforms(t *testing.T) {
	f := newPlatformsFixture()
	f.hold("Binance", 100)
	f.hold("binance", 50)
	f.holdings.holdings["other"] = model.Holding{Id: model.NewHoldingId(), UserId: model.NewUserId(uuid.New()),
		Platform: model.MustPlatformName("Nexo"), AssetClass: model.MustAssetClass("Cash"), Value: model.ZeroMoney}
	broker := model.MustPlatformType("Exchange")
	f.looks.settings[f.user.String()+"/binance"] = model.PlatformSettings{UserId: f.user, Key: "binance", Type: &broker, AvatarText: ptr("₿")}

	platforms, err := f.svc.GetAllPlatforms(context.Background(), f.user)
	require.NoError(t, err)
	require.Len(t, platforms, 1) // only the caller's, one per key
	p := platforms[0]
	assert.Equal(t, "Binance", p.Name.Value())
	assert.Equal(t, "Exchange", p.Type.Value())
	assert.Equal(t, "₿", *p.AvatarText)
	assert.Nil(t, p.Color)
	assert.Equal(t, 2, p.Count)
	assert.Equal(t, "150.00", p.Value.String())
}

func TestPlatformService_CustomizeAndReset(t *testing.T) {
	f := newPlatformsFixture()
	f.hold("Binance", 100)
	ctx := context.Background()

	p, err := f.svc.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{
		UserId: f.user, Id: platformId("Binance"), Type: set(" Exchange "), AvatarText: set(" 🟡 "), Color: set("#F0B90B"),
	})
	require.NoError(t, err)
	assert.Equal(t, "Exchange", p.Type.Value())
	assert.Equal(t, "🟡", *p.AvatarText)
	assert.Equal(t, "#f0b90b", p.Color.Value())
	assert.Equal(t, 1, f.quota())

	// Sending what's there writes nothing.
	saves := f.looks.saves
	_, err = f.svc.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("binance"), Color: set("#f0b90b")})
	require.NoError(t, err)
	assert.Equal(t, saves, f.looks.saves)

	// Reset to default: null for each; nothing left to keep.
	p, err = f.svc.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{
		UserId: f.user, Id: platformId("Binance"), Type: inbound.Change[string]{Set: true},
		AvatarText: inbound.Change[string]{Set: true}, Color: inbound.Change[string]{Set: true},
	})
	require.NoError(t, err)
	assert.Equal(t, "Other", p.Type.Value())
	assert.Nil(t, p.AvatarText)
	assert.Nil(t, p.Color)
	assert.Empty(t, f.looks.settings)
	assert.Equal(t, 0, f.quota())

	// A blank type is no type, as null.
	_, err = f.svc.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("Binance"), Type: set("Bank")})
	require.NoError(t, err)
	p, err = f.svc.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("Binance"), Type: set("   ")})
	require.NoError(t, err)
	assert.Equal(t, "Other", p.Type.Value())
	assert.Empty(t, f.looks.settings)
}

func TestPlatformService_ChecksWhatsSent(t *testing.T) {
	f := newPlatformsFixture()
	f.hold("Binance", 100)
	ctx := context.Background()
	cases := []struct {
		cmd  inbound.UpdatePlatformCommand
		want error
	}{
		{inbound.UpdatePlatformCommand{AvatarText: set("ABC")}, model.ErrInvalidAvatarText},
		{inbound.UpdatePlatformCommand{AvatarText: set("  ")}, model.ErrInvalidAvatarText},
		{inbound.UpdatePlatformCommand{Color: set("#12345")}, model.ErrInvalidColor},
		{inbound.UpdatePlatformCommand{Name: ptr(" ")}, model.ErrBlankLabel},
		{inbound.UpdatePlatformCommand{Type: set(string(make([]byte, 41)) + "x")}, model.ErrLabelTooLong},
	}
	for _, c := range cases {
		c.cmd.UserId, c.cmd.Id = f.user, platformId("Binance")
		_, err := f.svc.UpdatePlatform(ctx, c.cmd)
		assert.ErrorIs(t, err, c.want)
	}
	// An emoji is one character, a flag too; two letters are fine.
	for _, text := range []string{"👨‍👩‍👧", "🇦🇷", "BN", "é"} {
		_, err := f.svc.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("Binance"), AvatarText: set(text)})
		assert.NoError(t, err, text)
	}
}

func TestPlatformService_UnknownPlatformIsNotFound(t *testing.T) {
	f := newPlatformsFixture()
	f.hold("Binance", 100)
	for _, id := range []string{platformId("Nexo"), "%%%", ""} {
		_, err := f.svc.UpdatePlatform(context.Background(), inbound.UpdatePlatformCommand{UserId: f.user, Id: id, Color: set("#000000")})
		var notFound appErrors.ResourceNotFoundError
		assert.ErrorAs(t, err, &notFound, id)
	}
}

func TestPlatformService_RespellRenamesEveryHolding(t *testing.T) {
	f := newPlatformsFixture()
	a, b := f.hold("binance", 1), f.hold("BINANCE", 2)
	p, err := f.svc.UpdatePlatform(context.Background(), inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("Binance"), Name: ptr("Binance")})
	require.NoError(t, err)
	assert.Equal(t, "Binance", p.Name.Value())
	assert.Equal(t, "Binance", f.holdings.holdings[a.Id.String()].Platform.Value())
	assert.Equal(t, "Binance", f.holdings.holdings[b.Id.String()].Platform.Value())
}

func TestPlatformService_RenameTakesItsLookAlong(t *testing.T) {
	f := newPlatformsFixture()
	a := f.hold("Binance US", 10)
	ctx := context.Background()
	_, err := f.svc.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("Binance US"), AvatarText: set("BU")})
	require.NoError(t, err)
	// A look kept from an older platform with the new name gives way.
	f.looks.settings[f.user.String()+"/binance global"] = model.PlatformSettings{UserId: f.user, Key: "binance global", AvatarText: ptr("X")}
	f.quotas.counts[f.user.String()+"/platform_settings"]++

	p, err := f.svc.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("Binance US"), Name: ptr("Binance Global"), Color: set("#000000")})
	require.NoError(t, err)
	assert.Equal(t, "Binance Global", p.Name.Value())
	assert.Equal(t, "BU", *p.AvatarText)
	assert.Equal(t, "#000000", p.Color.Value())
	assert.Equal(t, "Binance Global", f.holdings.holdings[a.Id.String()].Platform.Value())
	_, old := f.looks.get(f.user, "Binance US")
	assert.False(t, old)
	assert.Equal(t, 1, f.quota())
}

func TestPlatformService_RenameOntoAnotherPlatformIsAMerge(t *testing.T) {
	f := newPlatformsFixture()
	f.hold("Binance", 100)
	us := f.hold("Binance US", 10)
	ctx := context.Background()
	_, err := f.svc.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("Binance US"), AvatarText: set("U")})
	require.NoError(t, err)
	_, err = f.svc.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("Binance"), Color: set("#f0b90b")})
	require.NoError(t, err)

	_, err = f.svc.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("Binance US"), Name: ptr("BINANCE")})
	assert.Equal(t, "platform-exists", conflictSlug(err))
	assert.Equal(t, `There's already a platform named "Binance"`, err.Error())

	p, err := f.svc.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("Binance US"), Name: ptr("BINANCE"), MergeIfExists: true})
	require.NoError(t, err)
	assert.Equal(t, "Binance", p.Name.Value(), "spelled as the platform merged into")
	assert.Equal(t, 2, p.Count)
	assert.Equal(t, "#f0b90b", p.Color.Value())
	assert.Nil(t, p.AvatarText)
	assert.Equal(t, "Binance", f.holdings.holdings[us.Id.String()].Platform.Value())
	assert.Equal(t, 1, f.quota())
}

func TestPlatformService_CapsCustomizedPlatforms(t *testing.T) {
	f := newPlatformsFixture()
	f.hold("Binance", 100)
	f.quotas.counts[f.user.String()+"/platform_settings"] = model.MaxPlatformSettingsPerUser
	_, err := f.svc.UpdatePlatform(context.Background(), inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("Binance"), AvatarText: set("B")})
	var limit appErrors.LimitExceededError
	require.ErrorAs(t, err, &limit)
	assert.Equal(t, "You can customize up to 1000 platforms. Reset one to customize another.", limit.Message)
	assert.Empty(t, f.looks.settings)
}

func TestPlatformService_LookIsKeptWhenEmptiedAndUsedAgain(t *testing.T) {
	f := newPlatformsFixture()
	h := f.hold("Nexo", 100)
	ctx := context.Background()
	_, err := f.svc.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("Nexo"), AvatarText: set("NX")})
	require.NoError(t, err)

	delete(f.holdings.holdings, h.Id.String())
	platforms, err := f.svc.GetAllPlatforms(ctx, f.user)
	require.NoError(t, err)
	assert.Empty(t, platforms)

	f.hold("nexo", 5)
	platforms, err = f.svc.GetAllPlatforms(ctx, f.user)
	require.NoError(t, err)
	require.Len(t, platforms, 1)
	assert.Equal(t, "NX", *platforms[0].AvatarText)
}

func TestPlatformService_ReadFailures(t *testing.T) {
	f := newPlatformsFixture()
	f.hold("Nexo", 1)
	f.looks.findErr = assert.AnError
	_, err := f.svc.GetAllPlatforms(context.Background(), f.user)
	assert.ErrorIs(t, err, assert.AnError)
	_, err = f.svc.UpdatePlatform(context.Background(), inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("Nexo"), Color: set("#000000")})
	assert.ErrorIs(t, err, assert.AnError)
}

func TestPlatformService_WriteFailuresRollBack(t *testing.T) {
	ctx := context.Background()
	for _, cmd := range []inbound.UpdatePlatformCommand{
		{Name: ptr("NEXO")},
		{Name: ptr("Nexo Pro")},
		{Name: ptr("Binance"), MergeIfExists: true},
	} {
		f := newPlatformsFixture()
		h := f.hold("Nexo", 10)
		f.hold("Binance", 1)
		f.holdings.reassignErr = assert.AnError
		cmd.UserId, cmd.Id = f.user, platformId("Nexo")
		_, err := f.svc.UpdatePlatform(ctx, cmd)
		assert.ErrorIs(t, err, assert.AnError)
		assert.Equal(t, "Nexo", f.holdings.holdings[h.Id.String()].Platform.Value())
	}

	f := newPlatformsFixture()
	f.hold("Nexo", 10)
	f.quotas.reserveErr = assert.AnError
	_, err := f.svc.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("Nexo"), Color: set("#000000")})
	assert.ErrorIs(t, err, assert.AnError)
	f.quotas.reserveErr = nil
	_, err = f.svc.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("Nexo"), Color: set("#000000")})
	require.NoError(t, err)
	f.quotas.releaseErr = assert.AnError
	_, err = f.svc.UpdatePlatform(ctx, inbound.UpdatePlatformCommand{UserId: f.user, Id: platformId("Nexo"), Color: inbound.Change[string]{Set: true}})
	assert.ErrorIs(t, err, assert.AnError)
	_, kept := f.looks.get(f.user, "Nexo")
	assert.True(t, kept, "rolled back")
}
