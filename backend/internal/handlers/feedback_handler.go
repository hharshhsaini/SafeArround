package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/obsidiannnn/Safe-Around/backend/internal/models"
	"github.com/obsidiannnn/Safe-Around/backend/internal/services"
)

type FeedbackHandler struct {
	feedbackService *services.FeedbackService
}

func NewFeedbackHandler(feedbackService *services.FeedbackService) *FeedbackHandler {
	return &FeedbackHandler{
		feedbackService: feedbackService,
	}
}

type SubmitFeedbackRequest struct {
	AlertID      string `json:"alert_id" binding:"required"`
	ResponderID  *uint  `json:"responder_id,omitempty"`
	Rating       int    `json:"rating" binding:"required,min=1,max=5"`
	Feedback     string `json:"feedback,omitempty"`
	SummaryNote  string `json:"summary_note,omitempty"`
	WasHelpful   *bool  `json:"was_helpful,omitempty"`
	FeedbackType string `json:"feedback_type,omitempty"`
}

// SubmitFeedback handles POST /api/v1/feedback
func (h *FeedbackHandler) SubmitFeedback(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	var req SubmitFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	alertID, err := uuid.Parse(req.AlertID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alert ID"})
		return
	}

	feedbackType := req.FeedbackType
	if feedbackType == "" {
		feedbackType = "resolution"
	}

	feedback := &models.UserFeedback{
		AlertID:      alertID,
		UserID:       userID.(uint),
		ResponderID:  req.ResponderID,
		Rating:       req.Rating,
		Feedback:     req.Feedback,
		SummaryNote:  req.SummaryNote,
		WasHelpful:   req.WasHelpful,
		FeedbackType: feedbackType,
	}

	if err := h.feedbackService.SubmitFeedback(feedback); err != nil {
		switch {
		case errors.Is(err, services.ErrFeedbackNotAllowed):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		case errors.Is(err, services.ErrNotAResponder):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, services.ErrAlreadyRated):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to submit feedback"})
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Feedback submitted successfully",
		"feedback_id": feedback.ID,
	})
}

// GetUserRatings handles GET /api/v1/users/:id/ratings
func (h *FeedbackHandler) GetUserRatings(c *gin.Context) {
	userIDStr := c.Param("id")
	userID, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	// Anyone may see the totals; individual feedback is for the rated user only.
	callerID, _ := c.Get("user_id")
	includeRecent := callerID == uint(userID)

	stats, err := h.feedbackService.GetUserRatingStats(uint(userID), includeRecent)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// GetTopRatedUsers handles GET /api/v1/users/top-rated
func (h *FeedbackHandler) GetTopRatedUsers(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "10")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 100 {
		limit = 10
	}

	users, err := h.feedbackService.GetTopRatedUsers(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get top rated users"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"users": users,
		"count": len(users),
	})
}