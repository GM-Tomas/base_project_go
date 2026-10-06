package app

import (
	"context"
	"fmt"
	"log"
	"net/http"

	appService "github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	appHttp "github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	mongopersistence "github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/outbound/persistence/mongo"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/config"
)

type App struct {
	Handler http.Handler
	Cleanup func()
}

func BuildApp(ctx context.Context, cfg config.Config) (*App, error) {
	// What every deployment's router has, previews included.
	routes := appHttp.RouterParams{
		AllowedOrigins: cfg.AllowedOrigins,
		HealthHandler:  appHttp.NewHealthHandler(),
		DocsPassword:   cfg.DocsPassword,
		HideDocs:       cfg.HideDocs,
	}
	if cfg.Preview {
		// MONGODB_URI is production's: a preview never connects, whatever reaches it.
		log.Println("Vercel preview: not connecting to MongoDB; API routes answer 503.")
		routes.NoData = true
		return &App{Handler: appHttp.NewRouter(routes), Cleanup: func() {}}, nil
	}

	// A failed connection is returned, not swallowed: cmd/api (also the Vercel entrypoint) exits on it.
	log.Printf("Connecting to MongoDB (database: %s)...", cfg.MongoDBName)
	mongoDB, err := mongopersistence.NewMongoDB(ctx, cfg.MongoDBURI, cfg.MongoDBName)
	if err != nil {
		return nil, fmt.Errorf("connecting to MongoDB: %w", err)
	}
	cleanup := func() {
		_ = mongoDB.Close(context.Background())
	}
	log.Println("MongoDB connection established successfully.")

	holdingRepo := mongopersistence.NewMongoHoldingRepository(mongoDB)
	debtRepo := mongopersistence.NewMongoDebtRepository(mongoDB)
	platformRepo := mongopersistence.NewMongoPlatformRepository(mongoDB)
	snapshotRepo := mongopersistence.NewMongoSnapshotRepository(mongoDB)
	movementRepo := mongopersistence.NewMongoMovementRepository(mongoDB)
	quotaRepo := mongopersistence.NewMongoQuotaRepository(mongoDB)
	transactions := mongopersistence.NewMongoTransactionManager(mongoDB)
	wealthAggAdapter := mongopersistence.NewMongoWealthAggregationAdapter(mongoDB)
	classSettingsRepo := mongopersistence.NewMongoAssetClassSettingsRepository(mongoDB)
	platformSettingsRepo := mongopersistence.NewMongoPlatformSettingsRepository(mongoDB)
	classDefaults := appService.NewClassDefaults(cfg.DefaultAssetClasses, cfg.LiquidAssetClasses)

	// Application Services
	holdingService := appService.NewHoldingService(transactions, holdingRepo, platformRepo, classSettingsRepo, movementRepo, quotaRepo, appService.RealClock)
	debtService := appService.NewDebtService(transactions, debtRepo, movementRepo, quotaRepo, appService.RealClock)
	movementService := appService.NewMovementService(transactions, holdingRepo, platformRepo, debtRepo, movementRepo, quotaRepo, appService.RealClock)
	platformService := appService.NewPlatformService(transactions, platformRepo, holdingRepo, platformSettingsRepo, quotaRepo, appService.RealClock)
	assetClassService := appService.NewAssetClassService(transactions, holdingRepo, classSettingsRepo, quotaRepo, wealthAggAdapter, classDefaults, appService.RealClock)
	snapshotService := appService.NewSnapshotService(snapshotRepo, wealthAggAdapter, appService.RealClock)
	projectionService := appService.NewProjectionService(wealthAggAdapter, debtRepo, classSettingsRepo, appService.RealClock)
	wealthService := appService.NewWealthQueryService(
		wealthAggAdapter,
		snapshotRepo,
		classSettingsRepo,
		platformSettingsRepo,
		appService.RealClock,
		classDefaults,
	)

	// HTTP Handlers
	routes.HoldingHandler = appHttp.NewHoldingHandler(holdingService)
	routes.DebtHandler = appHttp.NewDebtHandler(debtService)
	routes.PreferencesHandler = appHttp.NewPreferencesHandler(
		appService.NewPreferencesService(mongopersistence.NewMongoPreferencesRepository(mongoDB)))
	routes.MovementHandler = appHttp.NewMovementHandler(movementService)
	routes.PlatformHandler = appHttp.NewPlatformHandler(platformService)
	routes.AssetClassHandler = appHttp.NewAssetClassHandler(assetClassService)
	routes.WealthHandler = appHttp.NewWealthHandler(wealthService, snapshotService, projectionService)
	routes.SwaggerHandler = appHttp.NewSwaggerHandler()

	// Supabase JWT validation. The JWKS is fetched lazily, so this works offline too.
	supabaseValidator, err := middleware.NewSupabaseJWTValidator(
		cfg.JWKSetURI,
		cfg.AuthIssuer,
		cfg.AuthAudience,
	)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("configuring JWT validation: %w", err)
	}
	var jwtValidator middleware.JWTValidator = supabaseValidator
	if cfg.DevUserID != "" {
		devUser, err := model.ParseUserId(cfg.DevUserID)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("AUTH_DEV_USER_ID: %w", err)
		}
		log.Printf("WARNING: requests without a token act as user %s (AUTH_DEV_USER_ID); tokens are still verified", cfg.DevUserID)
		jwtValidator = middleware.DevValidator{UserId: devUser, Tokens: supabaseValidator}
	}

	routes.JWTValidator = jwtValidator

	return &App{
		Handler: appHttp.NewRouter(routes),
		Cleanup: cleanup,
	}, nil
}
