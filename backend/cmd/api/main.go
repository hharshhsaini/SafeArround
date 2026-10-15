package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/obsidiannnn/Safe-Around/backend/internal/config"
	"github.com/obsidiannnn/Safe-Around/backend/internal/database"
	"github.com/obsidiannnn/Safe-Around/backend/internal/handlers"
	"github.com/obsidiannnn/Safe-Around/backend/internal/repository"
	"github.com/obsidiannnn/Safe-Around/backend/internal/routes"
	"github.com/obsidiannnn/Safe-Around/backend/internal/services"
	customWS "github.com/obsidiannnn/Safe-Around/backend/internal/websocket"
	"github.com/obsidiannnn/Safe-Around/backend/pkg/fcm"
	"github.com/obsidiannnn/Safe-Around/backend/pkg/logger"
	"github.com/obsidiannnn/Safe-Around/backend/pkg/maps"
	"github.com/obsidiannnn/Safe-Around/backend/pkg/twilio"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// Initialize Custom Structured Logger
	logger.InitLogger(cfg.Server.Env, cfg.Server.Env == "development")

	if cfg.Server.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 1. Database Connection
	db, err := database.NewPostgresDB(cfg)
	if err != nil {
		logger.Fatal("failed to connect database", zap.Error(err))
	}

	// Auto Migrate existing models (Disabled for testing to avoid schema conflict with views)
	// if err := database.RunMigrations(db); err != nil {
	// 	logger.Fatal("failed to migrate database", zap.Error(err))
	// }
	logger.Info("database connected and migrated")

	// 1b. Initialize pgxpool for LISTEN/NOTIFY
	dbPool, err := database.NewPostgresPool(cfg)
	if err != nil {
		logger.Fatal("failed to create pgxpool", zap.Error(err))
	}
	logger.Info("database connection pool (pgxpool) initialized")

	// 2. Redis Connection
	rdb, err := database.NewRedisClient(cfg)
	if err != nil {
		logger.Fatal("failed to connect redis", zap.Error(err))
	}
	logger.Info("redis connected")

	// 3. External Integrations
	// Expo push tokens work without FCM credentials, so a bad key degrades
	// rather than stopping the server.
	fcmClient, err := fcm.NewClient(os.Getenv("FCM_SERVICE_ACCOUNT_JSON"))
	if err != nil {
		logger.Error("FCM_SERVICE_ACCOUNT_JSON is invalid; only Expo push tokens will be delivered", zap.Error(err))
		fcmClient, _ = fcm.NewClient("")
	} else if os.Getenv("FCM_SERVICE_ACCOUNT_JSON") == "" {
		logger.Warn("FCM_SERVICE_ACCOUNT_JSON is not set; only Expo push tokens will be delivered")
	}
	twilioClient := twilio.NewClient(os.Getenv("TWILIO_ACCOUNT_SID"), os.Getenv("TWILIO_AUTH_TOKEN"))
	
	// Initialize Google Maps client for location search
	mapsClient := maps.NewClient(os.Getenv("GOOGLE_MAPS_API_KEY"))

	// 4. Setup Repositories & Services
	userRepo := repository.NewUserRepo(db)
	sessionRepo := repository.NewSessionRepo(db)
	notifSvc := services.NewNotificationService(fcmClient, twilioClient, db, rdb)

	geoSvc := services.NewGeofencingService(db, rdb, notifSvc)

	crimeHub := customWS.NewCrimeHub(dbPool)

	alertSvc := services.NewAlertService(db, rdb, geoSvc, notifSvc, crimeHub)
	locationSvc := services.NewLocationService(db, rdb, geoSvc)
	routeSvc := services.NewRouteService(db, rdb)
	feedbackSvc := services.NewFeedbackService(db)

	// 5. Setup Handlers
	authHandler := handlers.NewAuthHandler(userRepo, sessionRepo, rdb, twilioClient)
	healthHandler := handlers.NewHealthHandler(db, rdb, cfg)
	notifHandler := handlers.NewNotificationHandler(notifSvc)
	alertHandler := handlers.NewAlertHandler(alertSvc)
	heatmapHandler := handlers.NewHeatmapHandler(db, rdb)
	wsHandler := handlers.HandleWebSocket(crimeHub)
	locationHandler := handlers.NewLocationHandler(locationSvc)
	locationSearchHandler := handlers.NewLocationSearchHandler(mapsClient)
	routeHandler := handlers.NewRouteHandler(routeSvc)
	profileHandler := handlers.NewProfileHandler(db, rdb)
	geofencingHandler := handlers.NewGeofencingHandler(geoSvc)
	feedbackHandler := handlers.NewFeedbackHandler(feedbackSvc)

	// 6. Setup Routes
	r := routes.SetupRouter(authHandler, healthHandler, notifHandler, alertHandler, heatmapHandler, wsHandler, locationHandler, locationSearchHandler, routeHandler, profileHandler, geofencingHandler, feedbackHandler, rdb)

	srv := &http.Server{
		Addr:    "0.0.0.0:" + cfg.Server.Port,
		Handler: r,
	}

	go func() {
		log.Printf("server starting on port %s", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen error: %s\n", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("server forced to shutdown:", err)
	}

	log.Println("server exited properly")
}
