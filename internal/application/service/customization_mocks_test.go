package service_test

import (
	"context"
	"slices"
	"strings"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
)

// mockClassSettingsRepo keeps class settings in memory, by user and class.
type mockClassSettingsRepo struct {
	settings map[string]model.AssetClassSettings
	findErr  error
	saveErr  error
	saves    int
}

func newMockClassSettingsRepo() *mockClassSettingsRepo {
	return &mockClassSettingsRepo{settings: map[string]model.AssetClassSettings{}}
}

func classSettingsKey(userId model.UserId, class model.AssetClass) string {
	return userId.String() + "/" + class.Value()
}

func (m *mockClassSettingsRepo) FindAll(ctx context.Context, userId model.UserId) ([]model.AssetClassSettings, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	var list []model.AssetClassSettings
	for _, s := range m.settings {
		if s.UserId == userId {
			list = append(list, s)
		}
	}
	slices.SortFunc(list, func(a, b model.AssetClassSettings) int { return strings.Compare(a.Name.Value(), b.Name.Value()) })
	return list, nil
}

func (m *mockClassSettingsRepo) Save(ctx context.Context, s model.AssetClassSettings) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saves++
	m.settings[classSettingsKey(s.UserId, s.Name)] = s
	return nil
}

func (m *mockClassSettingsRepo) Delete(ctx context.Context, userId model.UserId, class model.AssetClass) (bool, error) {
	key := classSettingsKey(userId, class)
	_, ok := m.settings[key]
	delete(m.settings, key)
	return ok, nil
}

// get is the user's settings for the class, if any.
func (m *mockClassSettingsRepo) get(userId model.UserId, class string) (model.AssetClassSettings, bool) {
	s, ok := m.settings[classSettingsKey(userId, model.MustAssetClass(class))]
	return s, ok
}

// mockPlatformSettingsRepo keeps platform settings in memory, by user and key.
type mockPlatformSettingsRepo struct {
	settings map[string]model.PlatformSettings
	findErr  error
	saves    int
}

func newMockPlatformSettingsRepo() *mockPlatformSettingsRepo {
	return &mockPlatformSettingsRepo{settings: map[string]model.PlatformSettings{}}
}

func (m *mockPlatformSettingsRepo) FindAll(ctx context.Context, userId model.UserId) ([]model.PlatformSettings, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	var list []model.PlatformSettings
	for _, s := range m.settings {
		if s.UserId == userId {
			list = append(list, s)
		}
	}
	slices.SortFunc(list, func(a, b model.PlatformSettings) int { return strings.Compare(a.Key, b.Key) })
	return list, nil
}

func (m *mockPlatformSettingsRepo) Find(ctx context.Context, userId model.UserId, key string) (*model.PlatformSettings, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	s, ok := m.settings[userId.String()+"/"+key]
	if !ok {
		return nil, nil
	}
	return &s, nil
}

func (m *mockPlatformSettingsRepo) Save(ctx context.Context, s model.PlatformSettings) error {
	m.saves++
	m.settings[s.UserId.String()+"/"+s.Key] = s
	return nil
}

func (m *mockPlatformSettingsRepo) Delete(ctx context.Context, userId model.UserId, key string) (bool, error) {
	_, ok := m.settings[userId.String()+"/"+key]
	delete(m.settings, userId.String()+"/"+key)
	return ok, nil
}

func (m *mockPlatformSettingsRepo) get(userId model.UserId, name string) (model.PlatformSettings, bool) {
	s, ok := m.settings[userId.String()+"/"+model.PlatformKey(model.MustPlatformName(name))]
	return s, ok
}

// holdingsAggregation is the class breakdown of the mock's holdings, as the real adapter gives it.
type holdingsAggregation struct {
	mockWealthAggregationPort
	holdings *mockHoldingRepo
}

func (a *holdingsAggregation) ByAssetClass(ctx context.Context, userId model.UserId) ([]outbound.AssetClassAggregate, error) {
	totals := map[model.AssetClass]*outbound.AssetClassAggregate{}
	var order []model.AssetClass
	for _, h := range a.holdings.sorted(userId) {
		t, ok := totals[h.AssetClass]
		if !ok {
			t = &outbound.AssetClassAggregate{AssetClass: h.AssetClass, Value: model.ZeroMoney}
			totals[h.AssetClass] = t
			order = append(order, h.AssetClass)
		}
		t.Value = t.Value.Plus(h.Value)
		t.Count++
	}
	list := make([]outbound.AssetClassAggregate, len(order))
	for i, class := range order {
		list[i] = *totals[class]
	}
	return list, nil
}
