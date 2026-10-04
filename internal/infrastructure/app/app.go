package app

import (
	"context"
	"fmt"
	"log"
	"net/http"

	appService "github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	appHttp "github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/outbound/persistence"
	mongopersistence "github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/outbound/persistence/mongo"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/config"
)

type App struct {
	Handler http.Handler
	Cleanup func()
}

func BuildApp(ctx context.Context, cfg config.Config) (*App, error) {
	var (
		holdingRepo      outbound.HoldingRepository
		platformRepo     outbound.PlatformRepository
		snapshotRepo     outbound.SnapshotRepository
		wealthAggAdapter outbound.WealthAggregationPort
		cleanup          func() = func() {}
	)

	// A failed connection is returned, not swallowed: repositories holding a nil pool would turn
	// every request into a panic. Callers decide whether to exit (cmd/api) or retry (api/index.go).
	if cfg.DBType == "mongodb" {
		log.Printf("Connecting to MongoDB (database: %s)...", cfg.MongoDBName)
		mongoDB, err := mongopersistence.NewMongoDB(ctx, cfg.MongoDBURI, cfg.MongoDBName)
		if err != nil {
			return nil, fmt.Errorf("connecting to MongoDB: %w", err)
		}
		cleanup = func() {
			_ = mongoDB.Close(context.Background())
		}
		log.Println("MongoDB connection established successfully.")

		holdingRepo = mongopersistence.NewMongoHoldingRepository(mongoDB)
		platformRepo = mongopersistence.NewMongoPlatformRepository(mongoDB)
		snapshotRepo = mongopersistence.NewMongoSnapshotRepository(mongoDB)
		wealthAggAdapter = mongopersistence.NewMongoWealthAggregationAdapter(mongoDB)
	} else {
		log.Println("Connecting to PostgreSQL...")
		db, err := persistence.NewDB(ctx, cfg.DatabaseURL)
		if err != nil {
			return nil, fmt.Errorf("connecting to PostgreSQL: %w", err)
		}
		cleanup = func() {
			db.Close()
		}
		log.Println("PostgreSQL connection pool established successfully.")

		holdingRepo = persistence.NewPostgresHoldingRepository(db)
		platformRepo = persistence.NewPostgresPlatformRepository(db)
		snapshotRepo = persistence.NewPostgresSnapshotRepository(db)
		wealthAggAdapter = persistence.NewPostgresWealthAggregationAdapter(db)
	}

	// Application Services
	holdingService := appService.NewHoldingService(holdingRepo, platformRepo, appService.RealClock)
	platformService := appService.NewPlatformService(platformRepo)
	assetClassService := appService.NewAssetClassService(holdingRepo, cfg.DefaultAssetClasses)
	snapshotService := appService.NewSnapshotService(snapshotRepo, wealthAggAdapter, appService.RealClock)
	projectionService := appService.NewProjectionService(wealthAggAdapter, appService.RealClock)
	wealthService := appService.NewWealthQueryService(
		wealthAggAdapter,
		snapshotRepo,
		appService.RealClock,
		cfg.LiquidAssetClasses,
	)

	// HTTP Handlers
	healthHandler := appHttp.NewHealthHandler()
	holdingHandler := appHttp.NewHoldingHandler(holdingService)
	platformHandler := appHttp.NewPlatformHandler(platformService)
	assetClassHandler := appHttp.NewAssetClassHandler(assetClassService)
	wealthHandler := appHttp.NewWealthHandler(wealthService, snapshotService, projectionService)
	swaggerHandler := appHttp.NewSwaggerHandler()

	// Supabase JWT Validator
	jwtValidator, err := middleware.NewSupabaseJWTValidator(
		ctx,
		cfg.JWKSetURI,
		cfg.AuthIssuer,
		cfg.AuthAudience,
	)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("configuring JWT validation: %w", err)
	}

	// Router
	router := appHttp.NewRouter(appHttp.RouterParams{
		AllowedOrigins:    cfg.AllowedOrigins,
		JWTValidator:      jwtValidator,
		HealthHandler:     healthHandler,
		HoldingHandler:    holdingHandler,
		PlatformHandler:   platformHandler,
		AssetClassHandler: assetClassHandler,
		WealthHandler:     wealthHandler,
		SwaggerHandler:    swaggerHandler,
	})

	return &App{
		Handler: router,
		Cleanup: cleanup,
	}, nil
}
