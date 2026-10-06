package http

import (
	"net/http"

	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

type RouterParams struct {
	AllowedOrigins     []string
	JWTValidator       middleware.JWTValidator
	HealthHandler      *HealthHandler
	HoldingHandler     *HoldingHandler
	DebtHandler        *DebtHandler
	PreferencesHandler *PreferencesHandler
	MovementHandler    *MovementHandler
	PlatformHandler    *PlatformHandler
	AssetClassHandler  *AssetClassHandler
	WealthHandler      *WealthHandler
	SwaggerHandler     *SwaggerHandler
	DocsPassword       string // non-empty = docs behind basic auth (user "docs")
	HideDocs           bool
	// NoData answers every API route but health with 503: a Vercel preview, which has no database (see
	// config.Config.Preview). Only HealthHandler is needed then.
	NoData bool
}

func NewRouter(params RouterParams) http.Handler {
	r := chi.NewRouter()

	if params.SwaggerHandler == nil {
		params.SwaggerHandler = NewSwaggerHandler()
	}

	// Global Middlewares
	r.Use(middleware.RequestIDMiddleware)
	// Client IP for the access log: the right-most X-Forwarded-For entry, which Vercel overwrites on
	// every request. (chi's RealIP, used before, trusted a client-sent True-Client-IP first.)
	r.Use(chimiddleware.ClientIPFromXFFTrustedProxies(1))
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.CorsMiddleware(params.AllowedOrigins))
	// Per-user financial data: never cached by Vercel/proxies/browsers, never sniffed or framed.
	r.Use(chimiddleware.SetHeader("Cache-Control", "no-store"))
	r.Use(chimiddleware.SetHeader("X-Content-Type-Options", "nosniff"))
	r.Use(chimiddleware.SetHeader("X-Frame-Options", "DENY"))
	r.Use(chimiddleware.SetHeader("Referrer-Policy", "no-referrer"))
	r.Use(chimiddleware.RequestSize(64 << 10)) // largest real body (a holding) is well under 1 KB

	// Documentation & Swagger UI: public locally; on Vercel only behind DOCS_PASSWORD.
	if !params.HideDocs {
		r.Group(func(r chi.Router) {
			if params.DocsPassword != "" {
				r.Use(chimiddleware.BasicAuth("docs", map[string]string{"docs": params.DocsPassword}))
			}
			r.Get("/swagger", params.SwaggerHandler.ServeSwaggerUI)
			r.Get("/swagger/*", params.SwaggerHandler.ServeSwaggerUI)
			r.Get("/docs", params.SwaggerHandler.ServeSwaggerUI)
			r.Get("/docs/*", params.SwaggerHandler.ServeSwaggerUI)
			r.Get("/openapi.json", params.SwaggerHandler.ServeOpenAPIJSON)
			r.Get("/api/v1/openapi.json", params.SwaggerHandler.ServeOpenAPIJSON)
			r.Get("/docs/openapi.json", params.SwaggerHandler.ServeOpenAPIJSON)
		})
	}

	r.Route("/api/v1", func(r chi.Router) {
		// Public route
		r.Get("/health", params.HealthHandler.CheckHealth)

		if params.NoData {
			r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
				middleware.WriteProblem(w, r, http.StatusServiceUnavailable, "preview-without-data", "Service Unavailable",
					"Preview deployments have no database: previews of the app run on demo data instead.", nil)
			})
			return
		}

		// Protected routes
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthMiddleware(params.JWTValidator))

			// Exactly the surface the frontend consumes (base_project_fe/src/lib/api.ts).
			r.Get("/holdings", params.HoldingHandler.GetAllHoldings)
			r.Post("/holdings", params.HoldingHandler.CreateHolding)
			r.Put("/holdings/expected-returns", params.HoldingHandler.SetExpectedReturns)
			r.Patch("/holdings/{id}", params.HoldingHandler.UpdateHolding)
			r.Delete("/holdings/{id}", params.HoldingHandler.DeleteHolding)

			r.Get("/debts", params.DebtHandler.GetDebts)
			r.Post("/debts", params.DebtHandler.CreateDebt)
			r.Patch("/debts/{id}", params.DebtHandler.UpdateDebt)
			r.Delete("/debts/{id}", params.DebtHandler.DeleteDebt)

			r.Get("/preferences", params.PreferencesHandler.GetPreferences)
			r.Put("/preferences", params.PreferencesHandler.ReplacePreferences)

			r.Get("/movements", params.MovementHandler.ListMovements)
			r.Post("/movements", params.MovementHandler.RecordMovement)
			r.Delete("/movements/{id}", params.MovementHandler.RevertMovement)

			r.Get("/platforms", params.PlatformHandler.GetAllPlatforms)
			r.Patch("/platforms/{id}", params.PlatformHandler.UpdatePlatform)

			r.Get("/asset-classes", params.AssetClassHandler.GetAvailableAssetClasses)
			r.Post("/asset-classes", params.AssetClassHandler.CreateAssetClass)
			r.Patch("/asset-classes/{id}", params.AssetClassHandler.UpdateAssetClass)
			r.Delete("/asset-classes/{id}", params.AssetClassHandler.DeleteAssetClass)

			// Wealth
			r.Route("/wealth", func(r chi.Router) {
				r.Get("/summary", params.WealthHandler.GetSummary)
				r.Get("/estimate", params.WealthHandler.GetEstimate)
				r.Get("/snapshots", params.WealthHandler.GetSnapshots)
				r.Post("/snapshots", params.WealthHandler.CreateSnapshot)
				r.Delete("/snapshots/{id}", params.WealthHandler.DeleteSnapshot)
			})
		})
	})

	return r
}
