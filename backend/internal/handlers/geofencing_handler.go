package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/obsidiannnn/Safe-Around/backend/internal/services"
)

type GeofencingHandler struct {
	geoSvc *services.GeofencingService
}

func NewGeofencingHandler(svc *services.GeofencingService) *GeofencingHandler {
	return &GeofencingHandler{geoSvc: svc}
}

// CheckDangerZone handles GET /api/v1/geofencing/check
func (h *GeofencingHandler) CheckDangerZone(c *gin.Context) {
	latStr := c.Query("lat")
	lngStr := c.Query("lng")

	lat, err1 := strconv.ParseFloat(latStr, 64)
	lng, err2 := strconv.ParseFloat(lngStr, 64)

	if err1 != nil || err2 != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid lat/lng parameters"})
		return
	}
	if err := validateCoordinates(lat, lng); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	zone, err := h.geoSvc.CheckDangerZone(lat, lng)
	if err != nil {
		// Not found is fine, just means not in a danger zone
		c.JSON(http.StatusNotFound, gin.H{"success": true, "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    zone,
	})
}

// GetNearbyUsers handles GET /api/v1/geofencing/nearby-users
func (h *GeofencingHandler) GetNearbyUsers(c *gin.Context) {
	latStr := c.Query("lat")
	lngStr := c.Query("lng")
	radiusStr := c.DefaultQuery("radius", "1000")

	lat, err1 := strconv.ParseFloat(latStr, 64)
	lng, err2 := strconv.ParseFloat(lngStr, 64)
	radius, err3 := strconv.Atoi(radiusStr)
	if err1 != nil || err2 != nil || err3 != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid lat/lng/radius parameters"})
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

	users, err := h.geoSvc.GetNearbyUsers(lat, lng, radius)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Only the count is returned; user records would expose names and phone numbers.
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"count": len(users),
		},
	})
}

// UpdateLocation handles POST /api/v1/geofencing/location
func (h *GeofencingHandler) UpdateLocation(c *gin.Context) {
	// This can be a duplicate of LocationHandler.UpdateLocation but specifically for geofencing triggers
	// In this implementation, we can just return OK or call the service
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetDangerZones handles GET /api/v1/geofencing/zones
func (h *GeofencingHandler) GetDangerZones(c *gin.Context) {
	// For now return empty or implement spatial bounds query
	c.JSON(http.StatusOK, gin.H{"success": true, "data": []interface{}{}})
}
