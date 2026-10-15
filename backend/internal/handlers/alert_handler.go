package handlers

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/obsidiannnn/Safe-Around/backend/internal/models"
	"github.com/obsidiannnn/Safe-Around/backend/internal/services"
	"gorm.io/gorm"
)

type AlertHandler struct {
	alertService *services.AlertService
}

func NewAlertHandler(as *services.AlertService) *AlertHandler {
	return &AlertHandler{alertService: as}
}

// POST /api/v1/alerts
func (h *AlertHandler) CreateAlert(c *gin.Context) {
	// Extract userID from context (set by auth middleware)
	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	userID := userIDVal.(uint)

	var req struct {
		Latitude   float64 `json:"latitude" binding:"required"`
		Longitude  float64 `json:"longitude" binding:"required"`
		AlertType  string  `json:"alert_type"`
		SilentMode bool    `json:"silent_mode"`
		Metadata   string  `json:"metadata"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateCoordinates(req.Latitude, req.Longitude); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	createReq := services.CreateAlertRequest{
		UserID: userID,
		Location: models.Location{
			Latitude:  req.Latitude,
			Longitude: req.Longitude,
		},
		AlertType:  req.AlertType,
		SilentMode: req.SilentMode,
		Metadata:   req.Metadata,
	}

	if createReq.AlertType == "" {
		createReq.AlertType = "emergency"
	}

	alert, err := h.alertService.CreateAlert(c.Request.Context(), createReq)
	if err != nil {
		log.Printf("Failed to create alert: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create emergency alert"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"message": "Emergency alert created and broadcasted successfully",
		"data":    alert,
		"alert":   alert,
	})
}

// GET /api/v1/alerts/:id
func (h *AlertHandler) GetAlertDetails(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	alertID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alert ID format"})
		return
	}

	details, err := h.alertService.GetAlertDetails(c.Request.Context(), alertID, userIDVal.(uint))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Alert not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch alert details"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": details})
}

// POST /api/v1/alerts/:id/respond
func (h *AlertHandler) RespondToAlert(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	responderID := userIDVal.(uint)

	alertIDStr := c.Param("id")
	alertID, err := uuid.Parse(alertIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alert ID format"})
		return
	}

	var req struct {
		Latitude  float64 `json:"latitude" binding:"required"`
		Longitude float64 `json:"longitude" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateCoordinates(req.Latitude, req.Longitude); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	loc := models.Location{
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
	}

	err = h.alertService.AcceptAlert(c.Request.Context(), alertID, responderID, loc)
	if err != nil {
		log.Printf("Failed to accept alert: %v", err)
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "Alert not found"})
		// The code lets the app tell a repeat accept (safe to carry on) from an
		// alert it must not head to.
		case errors.Is(err, services.ErrAlertAlreadyAccepted):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "already_accepted"})
		case errors.Is(err, services.ErrAlertInactive):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "alert_inactive"})
		case errors.Is(err, services.ErrAlertSelfResponse):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "self_response"})
		case errors.Is(err, services.ErrAlertNotDispatched):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to accept alert response"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Response to alert logged successfully",
		"data": gin.H{
			"alertId": alertID.String(),
			"status":  "accepted",
		},
	})
}

// PATCH /api/v1/alerts/:id/status
func (h *AlertHandler) UpdateAlertStatus(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	userID := userIDVal.(uint)

	alertIDStr := c.Param("id")
	alertID, err := uuid.Parse(alertIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alert ID format"})
		return
	}

	var req struct {
		Status         string `json:"status" binding:"required"`
		ResolutionType string `json:"resolution_type"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Status == "resolved" {
		err = h.alertService.ResolveAlert(c.Request.Context(), alertID, userID, req.ResolutionType)
		if err != nil {
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				c.JSON(http.StatusNotFound, gin.H{"error": "Alert not found"})
			case errors.Is(err, services.ErrAlertAccessDenied):
				c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			case errors.Is(err, services.ErrAlertInactive):
				c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to resolve alert"})
			}
			return
		}

		alert, alertErr := h.alertService.GetOwnedAlert(c.Request.Context(), alertID, userID)
		if alertErr != nil {
			c.JSON(http.StatusOK, gin.H{"success": true, "message": "Alert resolved successfully"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Alert resolved successfully",
			"data":    alert,
		})
		return
	}

	if req.Status == "cancelled" {
		err = h.alertService.CancelAlert(c.Request.Context(), alertID, userID)
		if err != nil {
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				c.JSON(http.StatusNotFound, gin.H{"error": "Alert not found"})
			case errors.Is(err, services.ErrAlertAccessDenied):
				c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			case errors.Is(err, services.ErrAlertInactive):
				c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cancel alert"})
			}
			return
		}

		alert, alertErr := h.alertService.GetOwnedAlert(c.Request.Context(), alertID, userID)
		if alertErr != nil {
			c.JSON(http.StatusOK, gin.H{"success": true, "message": "Alert cancelled successfully"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Alert cancelled successfully",
			"data":    alert,
		})
		return
	}

	c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported status update. Use 'resolved' or 'cancelled'."})
}

// POST /api/v1/alerts/:id/escalate
func (h *AlertHandler) EscalateAlert(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	userID := userIDVal.(uint)

	alertIDStr := c.Param("id")
	alertID, err := uuid.Parse(alertIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alert ID format"})
		return
	}

	var req struct {
		EscalationType string `json:"escalation_type" binding:"required"` // police, ambulance
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err = h.alertService.EscalateToEmergencyServices(alertID, userID, req.EscalationType)
	if err != nil {
		log.Printf("Escalation failed: %v", err)
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "Alert not found"})
		case errors.Is(err, services.ErrAlertAccessDenied):
			c.JSON(http.StatusForbidden, gin.H{"error": "Only the requester or an active responder can escalate this alert"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Emergency escalation dispatch failed"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":          "Escalation recorded and emergency contacts alerted. SafeAround cannot contact emergency services for you: call 112 now.",
		"emergency_number": "112",
	})
}

// GET    /api/v1/alerts/active
func (h *AlertHandler) GetActiveAlerts(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	alerts, err := h.alertService.GetActiveAlerts(c.Request.Context(), userIDVal.(uint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch active alerts"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": alerts})
}

// GET    /api/v1/alerts/history
func (h *AlertHandler) GetAlertHistory(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	userID := userIDVal.(uint)

	alerts, err := h.alertService.GetAlertHistory(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch alert history"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": alerts})
}
