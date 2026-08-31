package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	appService "github.com/GM-Tomas/base_project_go/internal/application/service"
	appHttp "github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/outbound/persistence"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/config"
)

func main() {
	cfg := config.LoadConfig()

	log.Printf("Starting BASE Wealth API (Go) on port %s...", cfg.Port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize Database Pool
	db, err := persistence.NewDB(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Printf("WARNING: Database connection could not be established immediately (%v). Running with deferred connection...", err)
	} else {
		defer db.Close()
		log.Println("Database connection pool established successfully.")
	}

	// Adapters & Repositories
	holdingRepo := persistence.NewPostgresHoldingRepository(db)
	platformRepo := persistence.NewPostgresPlatformRepository(db)
	snapshotRepo := persistence.NewPostgresSnapshotRepository(db)
	wealthAggAdapter := persistence.NewPostgresWealthAggregationAdapter(db)

	// Application Services
	holdingService := appService.NewHoldingService(holdingRepo, platformRepo, appService.RealClock)
	platformService := appService.NewPlatformService(platformRepo, appService.RealClock)
	assetClassService := appService.NewAssetClassService(holdingRepo, cfg.DefaultAssetClasses)
	snapshotService := appService.NewSnapshotService(snapshotRepo, wealthAggAdapter, appService.RealClock)
	projectionService := appService.NewProjectionService(wealthAggAdapter, appService.RealClock)
	wealthService := appService.NewWealthQueryService(
		wealthAggAdapter,
		snapshotRepo,
		appService.RealClock,
		cfg.LiquidAssetClasses,
		cfg.DefaultFxUsdArs,
	)

	// HTTP Handlers
	healthHandler := appHttp.NewHealthHandler()
	holdingHandler := appHttp.NewHoldingHandler(holdingService)
	platformHandler := appHttp.NewPlatformHandler(platformService)
	assetClassHandler := appHttp.NewAssetClassHandler(assetClassService)
	wealthHandler := appHttp.NewWealthHandler(wealthService, snapshotService, projectionService)

	// Supabase JWT Validator
	jwtValidator := middleware.NewSupabaseJWTValidator(
		ctx,
		cfg.JWKSetURI,
		cfg.AuthIssuer,
		cfg.AuthAudience,
	)

	// Router
	router := appHttp.NewRouter(appHttp.RouterParams{
		AllowedOrigins:    cfg.AllowedOrigins,
		JWTValidator:      jwtValidator,
		HealthHandler:     healthHandler,
		HoldingHandler:    holdingHandler,
		PlatformHandler:   platformHandler,
		AssetClassHandler: assetClassHandler,
		WealthHandler:     wealthHandler,
	})

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Server runner
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("BASE Wealth API is listening on http://localhost:%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	// Graceful Shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		log.Fatalf("Fatal server error: %v", err)
	case sig := <-shutdown:
		log.Printf("Received signal %v, shutting down server...", sig)

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Graceful shutdown failed: %v, forcing close", err)
			_ = server.Close()
		}
		log.Println("Server shutdown completed.")
	}
}
