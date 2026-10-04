package app

import (
	"context"
	"fmt"
	"log"
	"net/http"

	appService "github.com/GM-Tomas/base_project_go/internal/application/service"
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
	// A failed connection is returned, not swallowed: callers decide whether to exit (cmd/api)
	// or retry on the next request (api/index.go).
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
	platformRepo := mongopersistence.NewMongoPlatformRepository(mongoDB)
	snapshotRepo := mongopersistence.NewMongoSnapshotRepository(mongoDB)
	wealthAggAdapter := mongopersistence.NewMongoWealthAggregationAdapter(mongoDB)

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
