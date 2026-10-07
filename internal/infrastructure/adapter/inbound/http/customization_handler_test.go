package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func classPath(name string) string {
	return "/api/v1/asset-classes/" + model.AssetClassId(model.MustAssetClass(name))
}

func platformPath(name string) string {
	return "/api/v1/platforms/" + model.PlatformId(model.PlatformKey(model.MustPlatformName(name)))
}

func holdingOf(t *testing.T, r http.Handler, name, class, platform string, value float64) dto.HoldingResponse {
	t.Helper()
	rec := do(t, r, "POST", "/api/v1/holdings", dto.CreateHoldingRequest{Name: name, AssetClass: class, Platform: platform, ValueUsd: value})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return decode[dto.HoldingResponse](t, rec)
}

// doRaw sends body as it is (malformed, say).
func doRaw(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func problemOf(t *testing.T, rec interface{ Result() *http.Response }) middleware.ProblemDetail {
	t.Helper()
	var p middleware.ProblemDetail
	require.NoError(t, json.NewDecoder(rec.Result().Body).Decode(&p))
	return p
}

func TestAssetClassHandler_ListsTheUsersClasses(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))
	holdingOf(t, r, "BTC", "Crypto", "Binance", 1000)
	holdingOf(t, r, "Painting", "Art", "Home", 250)

	rec := do(t, r, "GET", "/api/v1/asset-classes", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	res := decode[dto.AvailableAssetClassesResponse](t, rec)
	assert.Equal(t, []string{"Cash", "Fixed Income", "Index Fund", "Equity", "Crypto"}, res.Defaults)
	assert.ElementsMatch(t, []string{"Crypto", "Art"}, res.InUse)
	assert.Equal(t, []string{"Cash", "Fixed Income", "Index Fund", "Equity", "Crypto", "Art"}, res.All)
	require.Len(t, res.Classes, 6)
	assert.Equal(t, dto.AssetClassResponse{Id: "Q3J5cHRv", Name: "Crypto", Liquid: true, IsDefault: true, HoldingsCount: 1, ValueUsd: 1000},
		res.Classes[4])
	assert.Equal(t, dto.AssetClassResponse{Id: "QXJ0", Name: "Art", HoldingsCount: 1, ValueUsd: 250}, res.Classes[5])

	// null, not absent, for what's not set.
	assert.Contains(t, do(t, r, "GET", "/api/v1/asset-classes", nil).Body.String(), `"color":null,"liquid":true,"expectedReturnPct":null`)
}

func TestAssetClassHandler_CreateUpdateDelete(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))

	rec := do(t, r, "POST", "/api/v1/asset-classes", json.RawMessage(`{"name": " Real Estate ", "color": "#AABBCC", "liquid": false, "expectedReturnPct": 6}`))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	created := decode[dto.AssetClassResponse](t, rec)
	assert.Equal(t, "/api/v1/asset-classes/"+created.Id, rec.Header().Get("Location"))
	assert.Equal(t, "Real Estate", created.Name)
	assert.Equal(t, "#aabbcc", *created.Color)
	assert.Equal(t, 6.0, *created.ExpectedReturnPct)

	rec = do(t, r, "POST", "/api/v1/asset-classes", json.RawMessage(`{"name": "Real Estate"}`))
	require.Equal(t, http.StatusConflict, rec.Code)
	p := problemOf(t, rec)
	assert.Equal(t, middleware.ProblemBaseURI+"/class-exists", p.Type)
	assert.Equal(t, `There's already a class named "Real Estate"`, p.Detail)

	h := holdingOf(t, r, "Flat", "Real Estate", "Deed", 90000)
	rec = do(t, r, "PATCH", classPath("Real Estate"), json.RawMessage(`{"name": "Property", "color": null, "liquid": true}`))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	renamed := decode[dto.AssetClassResponse](t, rec)
	assert.Equal(t, dto.AssetClassResponse{Id: model.AssetClassId(model.MustAssetClass("Property")), Name: "Property", Liquid: true,
		ExpectedReturnPct: ptrTo(6.0), HoldingsCount: 1, ValueUsd: 90000}, renamed)
	assert.Equal(t, "Property", r.holdings.holdings[h.Id].AssetClass.Value())
	// The holding counts with its class's return.
	holdings := decode[[]dto.HoldingResponse](t, do(t, r, "GET", "/api/v1/holdings", nil))
	assert.Nil(t, holdings[0].ExpectedReturnPct)
	assert.Equal(t, 6.0, *holdings[0].EffectiveReturnPct)

	rec = do(t, r, "PATCH", classPath("Property"), json.RawMessage(`{"name": "Equity"}`))
	require.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, middleware.ProblemBaseURI+"/class-exists", problemOf(t, rec).Type)
	rec = do(t, r, "PATCH", classPath("Property"), json.RawMessage(`{"name": "Equity", "mergeIfExists": true}`))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "Equity", decode[dto.AssetClassResponse](t, rec).Name)

	rec = do(t, r, "DELETE", classPath("Equity"), nil)
	require.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, middleware.ProblemBaseURI+"/class-in-use", problemOf(t, rec).Type)
	rec = do(t, r, "DELETE", classPath("Equity")+"?moveTo=Index%20Fund", nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, "Index Fund", r.holdings.holdings[h.Id].AssetClass.Value())
	res := decode[dto.AvailableAssetClassesResponse](t, do(t, r, "GET", "/api/v1/asset-classes", nil))
	assert.Equal(t, []string{"Cash", "Fixed Income", "Index Fund", "Crypto"}, res.All)

	assert.Equal(t, http.StatusNotFound, do(t, r, "DELETE", classPath("Equity"), nil).Code)
	assert.Equal(t, http.StatusNotFound, do(t, r, "PATCH", "/api/v1/asset-classes/%25%25", json.RawMessage(`{}`)).Code)
}

func TestAssetClassHandler_Validation(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))
	rec := do(t, r, "POST", "/api/v1/asset-classes", json.RawMessage(`{"name": " ", "color": "blue", "expectedReturnPct": 150}`))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	p := problemOf(t, rec)
	assert.Equal(t, []middleware.FieldError{
		{Field: "name", Message: "Name is required"},
		{Field: "color", Message: "color must be a hex color like #1a2b3c"},
		{Field: "expectedReturnPct", Message: "expectedReturnPct must be between -100 and 100"},
	}, p.Errors)

	rec = do(t, r, "POST", "/api/v1/asset-classes", json.RawMessage(`{"name": "`+strings.Repeat("a", 61)+`"}`))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "Name must be at most 60 characters", problemOf(t, rec).Detail)

	rec = do(t, r, "PATCH", classPath("Cash"), json.RawMessage(`{"name": null, "color": "#12"}`))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "Name is required; color must be a hex color like #1a2b3c", problemOf(t, rec).Detail)

	assert.Equal(t, http.StatusBadRequest, doRaw(r, "POST", "/api/v1/asset-classes", `{`).Code)
	assert.Equal(t, http.StatusBadRequest, doRaw(r, "PATCH", classPath("Cash"), `[`).Code)

	holdingOf(t, r, "Notes", "Cash", "Bank", 1)
	rec = do(t, r, "DELETE", classPath("Cash")+"?moveTo=Cash", nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "moveTo must be another class", problemOf(t, rec).Detail)
}

func TestPlatformHandler_ListsAndCustomizes(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))
	holdingOf(t, r, "BTC", "Crypto", "binance", 1000)
	holdingOf(t, r, "ETH", "Crypto", "Binance", 500)
	holdingOf(t, r, "USD", "Cash", "Binance US", 20)

	platforms := decode[[]dto.PlatformResponse](t, do(t, r, "GET", "/api/v1/platforms", nil))
	require.Len(t, platforms, 2)
	assert.Equal(t, "binance", platforms[0].Name)
	assert.Equal(t, model.PlatformId("binance"), platforms[0].Id)
	assert.Equal(t, 2, platforms[0].HoldingsCount)
	assert.Equal(t, 1500.0, platforms[0].ValueUsd)
	assert.Nil(t, platforms[0].AvatarText)
	assert.Contains(t, do(t, r, "GET", "/api/v1/platforms", nil).Body.String(), `"avatarText":null,"color":null,"textColor":null`)

	rec := do(t, r, "PATCH", platformPath("Binance"), json.RawMessage(`{"name": "Binance", "type": "Exchange", "avatarText": "🟡", "color": "#F0B90B", "textColor": "#1A1A1A"}`))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	p := decode[dto.PlatformResponse](t, rec)
	assert.Equal(t, "Binance", p.Name)
	assert.Equal(t, "Exchange", p.Type)
	assert.Equal(t, "🟡", *p.AvatarText)
	assert.Equal(t, "#f0b90b", *p.Color)
	assert.Equal(t, "#1a1a1a", *p.TextColor)

	// The summary shows them too.
	r.wealthAgg.byPlatform = []outbound.PlatformAggregate{{Key: "binance", Name: model.MustPlatformName("Binance"), Type: model.PlatformTypeOther,
		Value: model.MustMoneyFromFloat(1500), Count: 2}}
	summary := decode[dto.WealthSummaryResponse](t, do(t, r, "GET", "/api/v1/wealth/summary", nil))
	assert.Equal(t, "Exchange", summary.ByPlatform[0].Type)
	assert.Equal(t, "🟡", *summary.ByPlatform[0].AvatarText)
	assert.Equal(t, "#1a1a1a", *summary.ByPlatform[0].TextColor)

	rec = do(t, r, "PATCH", platformPath("Binance US"), json.RawMessage(`{"name": "BINANCE"}`))
	require.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, middleware.ProblemBaseURI+"/platform-exists", problemOf(t, rec).Type)
	rec = do(t, r, "PATCH", platformPath("Binance US"), json.RawMessage(`{"name": "BINANCE", "mergeIfExists": true}`))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	merged := decode[dto.PlatformResponse](t, rec)
	assert.Equal(t, 3, merged.HoldingsCount)
	assert.Equal(t, "Binance", merged.Name)

	rec = do(t, r, "PATCH", platformPath("Binance"), json.RawMessage(`{"type": null, "avatarText": null, "color": null, "textColor": null}`))
	require.Equal(t, http.StatusOK, rec.Code)
	reset := decode[dto.PlatformResponse](t, rec)
	assert.Equal(t, "Other", reset.Type)
	assert.Nil(t, reset.AvatarText)
	assert.Nil(t, reset.Color)
	assert.Nil(t, reset.TextColor)
	assert.Empty(t, r.looks.settings)

	assert.Equal(t, http.StatusNotFound, do(t, r, "PATCH", platformPath("Nexo"), json.RawMessage(`{"color": "#000000"}`)).Code)
}

func TestPlatformHandler_Validation(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))
	holdingOf(t, r, "BTC", "Crypto", "Binance", 1000)
	rec := do(t, r, "PATCH", platformPath("Binance"), json.RawMessage(`{"name": null, "type": "`+strings.Repeat("x", 41)+`", "avatarText": "ABC", "color": "red", "textColor": "#fff"}`))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, []middleware.FieldError{
		{Field: "name", Message: "Name is required"},
		{Field: "type", Message: "type must be at most 40 characters"},
		{Field: "avatarText", Message: "avatarText must be 1 or 2 characters (an emoji counts as one)"},
		{Field: "color", Message: "color must be a hex color like #1a2b3c"},
		{Field: "textColor", Message: "textColor must be a hex color like #1a2b3c"},
	}, problemOf(t, rec).Errors)
	assert.Equal(t, http.StatusBadRequest, doRaw(r, "PATCH", platformPath("Binance"), `{`).Code)
}

func TestCustomization_IsPerUser(t *testing.T) {
	alice, bob := model.NewUserId(uuid.New()), model.NewUserId(uuid.New())
	r := newTestRouter(alice)
	holdingOf(t, r, "BTC", "Crypto", "Binance", 1000)
	// Bob's holding, in the same store.
	bobs := model.Holding{Id: model.NewHoldingId(), UserId: bob, Name: "ETH", AssetClass: model.MustAssetClass("Art"),
		Platform: model.MustPlatformName("Nexo"), Value: model.MustMoneyFromFloat(5)}
	r.holdings.holdings[bobs.Id.String()] = bobs

	assert.Equal(t, http.StatusNotFound, do(t, r, "PATCH", classPath("Art"), json.RawMessage(`{"name": "Mine"}`)).Code)
	assert.Equal(t, http.StatusNotFound, do(t, r, "DELETE", classPath("Art")+"?moveTo=Cash", nil).Code)
	assert.Equal(t, http.StatusNotFound, do(t, r, "PATCH", platformPath("Nexo"), json.RawMessage(`{"name": "Mine"}`)).Code)
	assert.Equal(t, "Art", r.holdings.holdings[bobs.Id.String()].AssetClass.Value())
	assert.Equal(t, "Nexo", r.holdings.holdings[bobs.Id.String()].Platform.Value())

	// Alice renaming her Binance leaves Bob's alone, even with the same name.
	bobsBinance := model.Holding{Id: model.NewHoldingId(), UserId: bob, Name: "SOL", AssetClass: model.MustAssetClass("Crypto"),
		Platform: model.MustPlatformName("Binance"), Value: model.MustMoneyFromFloat(5)}
	r.holdings.holdings[bobsBinance.Id.String()] = bobsBinance
	require.Equal(t, http.StatusOK, do(t, r, "PATCH", platformPath("Binance"), json.RawMessage(`{"name": "Bnb"}`)).Code)
	require.Equal(t, http.StatusOK, do(t, r, "PATCH", classPath("Crypto"), json.RawMessage(`{"name": "Coins"}`)).Code)
	assert.Equal(t, "Binance", r.holdings.holdings[bobsBinance.Id.String()].Platform.Value())
	assert.Equal(t, "Crypto", r.holdings.holdings[bobsBinance.Id.String()].AssetClass.Value())
}
