package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type HeatmapHandler struct {
	db    *gorm.DB
	redis *redis.Client
}

func NewHeatmapHandler(db *gorm.DB, redis *redis.Client) *HeatmapHandler {
	return &HeatmapHandler{
		db:    db,
		redis: redis,
	}
}

// GET /api/v1/heatmap/data
// Returns crime data points for frontend heatmap
func (h *HeatmapHandler) GetHeatmapData(c *gin.Context) {
	// Parse bounds from query params
	north := c.Query("north")
	south := c.Query("south")
	east := c.Query("east")
	west := c.Query("west")

	// Query crimes in visible map bounds, evaluating dynamic time-decay factor for weight_pct
	query := `
        SELECT
            id,
            crime_type,
            severity,
            ST_Y(location::geometry) as latitude,
            ST_X(location::geometry) as longitude,
            occurred_at,
            location_name,
            GREATEST(0.0, 
                CASE severity 
                    WHEN 4 THEN 100.0 
                    WHEN 3 THEN 75.0 
                    WHEN 2 THEN 50.0 
                    ELSE 25.0 
                END 
                - 
                (EXTRACT(EPOCH FROM (NOW() - occurred_at)) / 86400.0 * 
                CASE 
                    WHEN crime_type IN ('murder', 'rape') THEN 0.1 
                    WHEN crime_type IN ('robbery', 'kidnapping', 'assault') THEN 0.5 
                    ELSE 2.0 
                END)
            ) as weight_pct
        FROM crime_incidents
        WHERE ST_Intersects(
            location::geometry,
            ST_MakeEnvelope($1, $2, $3, $4, 4326)
        )
        AND occurred_at > NOW() - INTERVAL '90 days'
        ORDER BY severity DESC, occurred_at DESC
        LIMIT 5000
    `

	var crimes []map[string]interface{}
	result := h.db.Raw(query, west, south, east, north).Scan(&crimes)
	
	if result.Error != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error":   "Failed to fetch crime data",
			"message": result.Error.Error(),
		})
		return
	}

	c.JSON(200, gin.H{
		"success": true,
		"data":    crimes,
		"count":   len(crimes),
	})
}

// GET /api/v1/heatmap/grid
// Returns aggregated grid data for smooth heatmap
func (h *HeatmapHandler) GetGridData(c *gin.Context) {
	north := c.Query("north")
	south := c.Query("south")
	east := c.Query("east")
	west := c.Query("west")

	// Get grid cells in bounds
	query := `
        SELECT
            grid_x,
            grid_y,
            crime_count,
            severity_sum,
            avg_severity
        FROM mv_crime_heatmap_grid
        WHERE grid_x BETWEEN FLOOR(? / 0.001) AND FLOOR(? / 0.001)
        AND grid_y BETWEEN FLOOR(? / 0.001) AND FLOOR(? / 0.001)
    `

	var gridCells []map[string]interface{}
	h.db.Raw(query, west, south, east, north).Scan(&gridCells)

	c.JSON(200, gin.H{
		"success": true,
		"data":    gridCells,
	})
}

// GET /api/v1/heatmap/tiles/:z/:x/:y
func (h *HeatmapHandler) GetTile(c *gin.Context) {
	// For backward compatibility, return an empty image or 404
	c.Status(http.StatusNotFound)
}

// GET /api/v1/heatmap/zone
func (h *HeatmapHandler) GetZoneInfo(c *gin.Context) {
	lat, latErr := strconv.ParseFloat(c.Query("lat"), 64)
	lng, lngErr := strconv.ParseFloat(c.Query("lng"), 64)
	if latErr != nil || lngErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid lat/lng parameters"})
		return
	}
	if err := validateCoordinates(lat, lng); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var stats struct {
		CrimeCount   int     `json:"crime_count"`
		SafetyRating float64 `json:"safety_rating"`
	}

	h.db.Raw(`
        SELECT COUNT(*) as crime_count 
        FROM crime_incidents 
        WHERE ST_DWithin(location, ST_MakePoint(?, ?)::geography, 1000)
    `, lng, lat).Scan(&stats)

	stats.SafetyRating = 5.0 - (float64(stats.CrimeCount) * 0.5)
	if stats.SafetyRating < 0 {
		stats.SafetyRating = 0
	}

	c.JSON(200, gin.H{"success": true, "data": stats})
}

// GET /api/v1/heatmap/crimes
func (h *HeatmapHandler) GetRecentCrimes(c *gin.Context) {
	var crimes []map[string]interface{}
	h.db.Raw(`SELECT * FROM crime_incidents ORDER BY occurred_at DESC LIMIT 50`).Scan(&crimes)
	c.JSON(200, gin.H{"success": true, "data": crimes})
}

// GET /api/v1/heatmap/statistics
func (h *HeatmapHandler) GetStatistics(c *gin.Context) {
	lat, latErr := strconv.ParseFloat(c.Query("lat"), 64)
	lng, lngErr := strconv.ParseFloat(c.Query("lng"), 64)
	if latErr != nil || lngErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid lat/lng parameters"})
		return
	}
	if err := validateCoordinates(lat, lng); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	cacheKey := statsCacheKey(lat, lng)
	if cached, ok := h.getCachedJSON(c.Request.Context(), cacheKey); ok {
		c.JSON(http.StatusOK, cached)
		return
	}

	type areaStats struct {
		CrimeCount   int64 `json:"crime_count"`
		SeveritySum  int64 `json:"severity_sum"`
		NearbyUsers  int64 `json:"nearby_users"`
		RecentAlerts int64 `json:"recent_alerts"`
	}

	var statsRow areaStats
	if err := h.db.Raw(`
		WITH target AS (
			SELECT ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography AS point
		),
		crime_stats AS (
			SELECT
				COUNT(*)::bigint AS crime_count,
				COALESCE(SUM(severity), 0)::bigint AS severity_sum
			FROM crime_incidents, target
			WHERE ST_DWithin(location::geography, target.point, 1000)
			  AND occurred_at > NOW() - INTERVAL '30 days'
			  AND verified = true
		),
		nearby_stats AS (
			SELECT COUNT(DISTINCT user_id)::bigint AS nearby_users
			FROM user_locations, target
			WHERE ST_DWithin(location::geography, target.point, 1000)
			  AND recorded_at > NOW() - INTERVAL '5 minutes'
		),
		alert_stats AS (
			SELECT COUNT(*)::bigint AS recent_alerts
			FROM emergency_alerts, target
			WHERE ST_DWithin(alert_location::geography, target.point, 1000)
			  AND created_at > NOW() - INTERVAL '24 hours'
			  AND alert_status IN ('active', 'responding')
		)
		SELECT
			crime_stats.crime_count,
			crime_stats.severity_sum,
			nearby_stats.nearby_users,
			alert_stats.recent_alerts
		FROM crime_stats, nearby_stats, alert_stats
	`, lng, lat).Scan(&statsRow).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load area statistics"})
		return
	}

	// Score penalizes both incident volume and severity, capped to the 0-100 UI range.
	safetyScore := int64(math.Round(100 - (float64(statsRow.CrimeCount) * 3.5) - (float64(statsRow.SeveritySum) * 1.5)))
	if safetyScore < 0 {
		safetyScore = 0
	} else if safetyScore > 100 {
		safetyScore = 100
	}

	stats := gin.H{
		"safetyScore":  safetyScore,
		"nearbyUsers":  statsRow.NearbyUsers,
		"recentAlerts": statsRow.RecentAlerts,
		"crimeRate":    math.Round((float64(statsRow.CrimeCount)/30.0)*10) / 10,
		"lastUpdated":  time.Now(),
	}

	h.cacheJSON(c.Request.Context(), cacheKey, stats, 15*time.Second)
	c.JSON(200, stats)
}

// POST /api/v1/heatmap/report
func (h *HeatmapHandler) ReportIncident(c *gin.Context) {
	userIDVal, _ := c.Get("user_id")

	var input struct {
		CrimeType   string  `json:"crime_type" binding:"required"`
		Severity    int     `json:"severity" binding:"required"`
		Latitude    float64 `json:"latitude" binding:"required"`
		Longitude   float64 `json:"longitude" binding:"required"`
		Description string  `json:"description"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateCoordinates(input.Latitude, input.Longitude); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if input.Severity < 1 || input.Severity > 4 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "severity must be between 1 and 4"})
		return
	}

	var availableColumns []string
	if err := h.db.Raw(`
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'crime_incidents'
	`).Scan(&availableColumns).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to inspect incident schema"})
		return
	}

	columnSet := make(map[string]bool, len(availableColumns))
	for _, column := range availableColumns {
		columnSet[column] = true
	}

	columns := make([]string, 0, 8)
	values := make([]string, 0, 8)
	args := make([]interface{}, 0, 8)

	addColumn := func(column, placeholder string, value ...interface{}) {
		if !columnSet[column] {
			return
		}
		columns = append(columns, column)
		values = append(values, placeholder)
		args = append(args, value...)
	}

	addColumn("type", "?", input.CrimeType)
	addColumn("crime_type", "?", input.CrimeType)
	addColumn("incident_type", "?", input.CrimeType)
	addColumn("severity", "?", input.Severity)
	addColumn("location", "ST_SetSRID(ST_MakePoint(?, ?), 4326)", input.Longitude, input.Latitude)
	addColumn("description", "?", input.Description)
	addColumn("occurred_at", "NOW()")
	addColumn("reported_at", "NOW()")
	addColumn("source", "?", "user_report")
	addColumn("verified", "?", false)
	if userID, ok := userIDVal.(uint); ok {
		addColumn("reported_by", "?", userID)
	}

	if len(columns) == 0 || (!columnSet["type"] && !columnSet["crime_type"] && !columnSet["incident_type"]) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "crime incidents schema is missing a type column"})
		return
	}

	query := `
        INSERT INTO crime_incidents (` + strings.Join(columns, ", ") + `)
        VALUES (` + strings.Join(values, ", ") + `)
    `
	if err := h.db.Exec(query, args...).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"success": true, "message": "Incident reported successfully"})
}

func statsCacheKey(lat, lng float64) string {
	return fmt.Sprintf("heatmap:stats:%0.4f:%0.4f", roundCoord(lat, 4), roundCoord(lng, 4))
}

func roundCoord(value float64, decimals int) float64 {
	pow := math.Pow10(decimals)
	return math.Round(value*pow) / pow
}

func (h *HeatmapHandler) getCachedJSON(ctx context.Context, cacheKey string) (gin.H, bool) {
	if h.redis == nil {
		return nil, false
	}

	cached, err := h.redis.Get(ctx, cacheKey).Result()
	if err != nil || cached == "" {
		return nil, false
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(cached), &payload); err != nil {
		return nil, false
	}

	return gin.H(payload), true
}

func (h *HeatmapHandler) cacheJSON(ctx context.Context, cacheKey string, payload gin.H, ttl time.Duration) {
	if h.redis == nil {
		return
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}

	h.redis.Set(ctx, cacheKey, encoded, ttl)
}
