package routes

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/obsidiannnn/Safe-Around/backend/internal/handlers"
	"github.com/obsidiannnn/Safe-Around/backend/internal/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

func SetupRouter(
	authHandler *handlers.AuthHandler,
	healthHandler *handlers.HealthHandler,
	notifHandler *handlers.NotificationHandler,
	alertHandler *handlers.AlertHandler,
	heatmapHandler *handlers.HeatmapHandler,
	wsHandler gin.HandlerFunc,
	locationHandler *handlers.LocationHandler,
	locationSearchHandler *handlers.LocationSearchHandler,
	routeHandler *handlers.RouteHandler,
	profileHandler *handlers.ProfileHandler,
	geofencingHandler *handlers.GeofencingHandler,
	feedbackHandler *handlers.FeedbackHandler,
	rdb *redis.Client,
) *gin.Engine {
	r := gin.New()

	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(middleware.SecurityHeadersMiddleware())
	r.Use(middleware.CorsMiddleware())
	r.Use(middleware.MetricsMiddleware())

	// Prometheus metrics scrape endpoint (unauthenticated – secure at the network level)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	// Public Health endpoints
	healthGroup := r.Group("/health")
	{
		healthGroup.GET("", healthHandler.GetHealth)
		healthGroup.GET("/ping", healthHandler.GetPing)
		healthGroup.GET("/readiness", healthHandler.GetReadiness)
	}

	// WebSockets Upgrade Endpoint (authenticated so events can be targeted per user)
	r.GET("/ws/crime", middleware.AuthRequired(rdb), wsHandler)

	// API Endpoints - applying global rate limit of 1000 requests / minute for development
	api := r.Group("/api/v1")
	api.Use(middleware.RateLimit(rdb, 1000, 60*time.Second))
	{
		SetupAuthRoutes(api, authHandler, rdb)

		// Notifications domain
		notifs := api.Group("/notifications")
		notifs.Use(middleware.AuthRequired(rdb))
		{
			notifs.POST("/register-token", notifHandler.RegisterDeviceToken)
			// There is deliberately no route for sending arbitrary pushes: any
			// signed-in user could have used it to spoof alerts.

			// Standard history fetches
			notifs.GET("/history", notifHandler.GetNotificationHistory)
			notifs.PUT("/:id/read", notifHandler.UpdateNotificationStatus)
			notifs.PATCH("/:id/read", notifHandler.UpdateNotificationStatus) // the app's client uses PATCH
		}

		// Emergency Alerts domain
		alerts := api.Group("/alerts")
		alerts.Use(middleware.AuthRequired(rdb))
		{
			alerts.POST("", alertHandler.CreateAlert)
			alerts.GET("/:id", alertHandler.GetAlertDetails)
			alerts.POST("/:id/respond", alertHandler.RespondToAlert)
			alerts.PATCH("/:id/status", alertHandler.UpdateAlertStatus)
			alerts.POST("/:id/escalate", alertHandler.EscalateAlert)
			alerts.GET("/active", alertHandler.GetActiveAlerts)
			alerts.GET("/history", alertHandler.GetAlertHistory)
		}

		// Heatmap domain
		heatmap := api.Group("/heatmap")
		{
			heatmap.GET("/data", heatmapHandler.GetHeatmapData)
			heatmap.GET("/grid", heatmapHandler.GetGridData)
			heatmap.GET("/tiles/:z/:x/:y", heatmapHandler.GetTile) // Keep for backward compatibility
			heatmap.GET("/zone", heatmapHandler.GetZoneInfo)
			heatmap.GET("/crimes", heatmapHandler.GetRecentCrimes)
			heatmap.GET("/statistics", heatmapHandler.GetStatistics)
			heatmap.POST("/report", middleware.AuthRequired(rdb), heatmapHandler.ReportIncident)
		}

		// Location domain
		location := api.Group("/location")
		location.Use(middleware.AuthRequired(rdb))
		{
			location.POST("", locationHandler.UpdateLocation)
			location.GET("/me", locationHandler.GetCurrentLocation)
			location.GET("/nearby", locationHandler.GetNearbyUsers)
			location.GET("/search", locationSearchHandler.SearchLocation)
			location.GET("/reverse", locationSearchHandler.ReverseGeocode)
		}

		// Safe Route Planning
		routesGroup := api.Group("/routes")
		routesGroup.Use(middleware.AuthRequired(rdb))
		{
			routesGroup.POST("/safe", routeHandler.GetSafeRoutes)
		}

		// User Profile & Contacts domain
		users := api.Group("/users")
		users.Use(middleware.AuthRequired(rdb))
		{
			users.GET("/profile", profileHandler.GetProfile)
			users.PUT("/profile", profileHandler.UpdateProfile)
			users.GET("/contacts", profileHandler.GetContacts)
			users.POST("/contacts", profileHandler.AddContact)
			users.DELETE("/contacts/:id", profileHandler.DeleteContact)
			users.GET("/:id/ratings", feedbackHandler.GetUserRatings)
			users.GET("/top-rated", feedbackHandler.GetTopRatedUsers)
		}

		// Feedback domain
		feedback := api.Group("/feedback")
		feedback.Use(middleware.AuthRequired(rdb))
		{
			feedback.POST("", feedbackHandler.SubmitFeedback)
		}

		// Geofencing domain
		geofencing := api.Group("/geofencing")
		geofencing.Use(middleware.AuthRequired(rdb))
		{
			geofencing.GET("/check", geofencingHandler.CheckDangerZone)
			geofencing.GET("/nearby-users", geofencingHandler.GetNearbyUsers)
			geofencing.POST("/location", geofencingHandler.UpdateLocation)
			geofencing.GET("/zones", geofencingHandler.GetDangerZones)
		}
	}

	return r
}
