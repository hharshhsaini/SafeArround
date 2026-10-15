package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/obsidiannnn/Safe-Around/backend/internal/models"
	"github.com/obsidiannnn/Safe-Around/backend/internal/services"
)

type LocationHandler struct {
	locationSvc *services.LocationService
}

func NewLocationHandler(svc *services.LocationService) *LocationHandler {
	return &LocationHandler{locationSvc: svc}
}

// UpdateLocation handles POST /api/v1/location
func (h *LocationHandler) UpdateLocation(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	userID := userIDVal.(uint)

	var req struct {
		Latitude       float64 `json:"latitude" binding:"required"`
		Longitude      float64 `json:"longitude" binding:"required"`
		Accuracy       float64 `json:"accuracy"`
		Altitude       float64 `json:"altitude"`
		Speed          float64 `json:"speed"`
		Heading        float64 `json:"heading"`
		BatteryLevel   string  `json:"battery_level"`
		NetworkType    string  `json:"network_type"`
		LocationSource string  `json:"location_source"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateCoordinates(req.Latitude, req.Longitude); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	loc := models.UserLocation{
		Location:       models.Location{Latitude: req.Latitude, Longitude: req.Longitude},
		Accuracy:       req.Accuracy,
		Altitude:       req.Altitude,
		Speed:          req.Speed,
		Heading:        req.Heading,
		BatteryLevel:   req.BatteryLevel,
		NetworkType:    req.NetworkType,
		LocationSource: req.LocationSource,
	}

	if err := h.locationSvc.UpdateUserLocation(userID, loc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update location"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// GetCurrentLocation handles GET /api/v1/location/me
func (h *LocationHandler) GetCurrentLocation(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	userID := userIDVal.(uint)

	loc, err := h.locationSvc.GetCurrentLocation(userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Location not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"latitude":    loc.Location.Latitude,
		"longitude":   loc.Location.Longitude,
		"accuracy":    loc.Accuracy,
		"recorded_at": loc.RecordedAt,
	})
}

// GetNearbyUsers handles GET /api/v1/location/nearby?lat=&lng=&radius=
func (h *LocationHandler) GetNearbyUsers(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	userID := userIDVal.(uint)

	latStr := c.Query("lat")
	lngStr := c.Query("lng")
	radiusStr := c.DefaultQuery("radius", "5000")

	lat, err1 := strconv.ParseFloat(latStr, 64)
	lng, err2 := strconv.ParseFloat(lngStr, 64)
	radius, err3 := strconv.Atoi(radiusStr)

	if err1 != nil || err2 != nil || err3 != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid lat/lng/radius parameters"})
		return
	}
	if err := validateCoordinates(lat, lng); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateRadius(radius, 50, 10000); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	cells, err := h.locationSvc.GetNearbyUserCells(lat, lng, radius, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Query failed"})
		return
	}

	count := 0
	for _, cell := range cells {
		count += cell.Count
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"data":          cells,
		"count":         count,
		"radius_meters": radius,
	})
}
