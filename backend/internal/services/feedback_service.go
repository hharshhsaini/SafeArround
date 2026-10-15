package services

import (
	"errors"
	"fmt"
	"time"

	"github.com/obsidiannnn/Safe-Around/backend/internal/models"
	"gorm.io/gorm"
)

var (
	ErrFeedbackNotAllowed = errors.New("you can only give feedback on your own alerts")
	ErrNotAResponder      = errors.New("that user did not respond to this alert")
	ErrAlreadyRated       = errors.New("you have already rated this responder for this alert")
)

type FeedbackService struct {
	db *gorm.DB
}

func NewFeedbackService(db *gorm.DB) *FeedbackService {
	return &FeedbackService{db: db}
}

// SubmitFeedback creates a new feedback entry and updates user ratings. Only
// the person who raised the alert may leave feedback, and a rated responder
// must have responded to that alert, once per alert.
func (s *FeedbackService) SubmitFeedback(feedback *models.UserFeedback) error {
	var alert models.EmergencyAlert
	if err := s.db.Select("id", "user_id").First(&alert, "id = ?", feedback.AlertID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrFeedbackNotAllowed
		}
		return err
	}
	if alert.UserID != feedback.UserID {
		return ErrFeedbackNotAllowed
	}

	if feedback.ResponderID != nil {
		var responses int64
		if err := s.db.Model(&models.AlertResponse{}).
			Where("alert_id = ? AND responder_user_id = ?", feedback.AlertID, *feedback.ResponderID).
			Count(&responses).Error; err != nil {
			return err
		}
		if responses == 0 {
			return ErrNotAResponder
		}

		var existing int64
		if err := s.db.Model(&models.UserFeedback{}).
			Where("alert_id = ? AND user_id = ? AND responder_id = ?", feedback.AlertID, feedback.UserID, *feedback.ResponderID).
			Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return ErrAlreadyRated
		}
	}

	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Create the feedback record
	if err := tx.Create(feedback).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to create feedback: %w", err)
	}

	// Update responder ratings if this is responder feedback
	if feedback.ResponderID != nil {
		if err := s.updateUserRatings(tx, *feedback.ResponderID); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update user ratings: %w", err)
		}
	}

	return tx.Commit().Error
}

// updateUserRatings recalculates and updates user rating statistics
func (s *FeedbackService) updateUserRatings(tx *gorm.DB, userID uint) error {
	var stats struct {
		TotalRatings   int64
		AverageRating  float64
		FiveStarCount  int64
		FourStarCount  int64
		ThreeStarCount int64
		TwoStarCount   int64
		OneStarCount   int64
	}

	// Calculate rating statistics
	if err := tx.Model(&models.UserFeedback{}).
		Select(`
			COUNT(*) as total_ratings,
			AVG(rating::decimal) as average_rating,
			COUNT(CASE WHEN rating = 5 THEN 1 END) as five_star_count,
			COUNT(CASE WHEN rating = 4 THEN 1 END) as four_star_count,
			COUNT(CASE WHEN rating = 3 THEN 1 END) as three_star_count,
			COUNT(CASE WHEN rating = 2 THEN 1 END) as two_star_count,
			COUNT(CASE WHEN rating = 1 THEN 1 END) as one_star_count
		`).
		Where("responder_id = ? AND deleted_at IS NULL", userID).
		Scan(&stats).Error; err != nil {
		return fmt.Errorf("failed to calculate rating statistics: %w", err)
	}

	// Update user record with new statistics
	updates := map[string]interface{}{
		"total_ratings":     stats.TotalRatings,
		"average_rating":    stats.AverageRating,
		"five_star_count":   stats.FiveStarCount,
		"four_star_count":   stats.FourStarCount,
		"three_star_count":  stats.ThreeStarCount,
		"two_star_count":    stats.TwoStarCount,
		"one_star_count":    stats.OneStarCount,
	}

	if err := tx.Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update user rating statistics: %w", err)
	}

	return nil
}

// GetUserRatingStats returns rating statistics for a user. Recent feedback is
// included only when includeRecent is set, which callers do for the rated
// user themselves.
func (s *FeedbackService) GetUserRatingStats(userID uint, includeRecent bool) (*UserRatingStats, error) {
	var user models.User
	if err := s.db.First(&user, userID).Error; err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	stats := &UserRatingStats{
		UserID:         user.ID,
		AverageRating:  user.AverageRating,
		TotalRatings:   user.TotalRatings,
		FiveStarCount:  user.FiveStarCount,
		FourStarCount:  user.FourStarCount,
		ThreeStarCount: user.ThreeStarCount,
		TwoStarCount:   user.TwoStarCount,
		OneStarCount:   user.OneStarCount,
	}
	if !includeRecent {
		return stats, nil
	}

	var recentFeedback []models.UserFeedback
	if err := s.db.Where("responder_id = ?", userID).
		Order("created_at DESC").
		Limit(10).
		Find(&recentFeedback).Error; err != nil {
		return nil, fmt.Errorf("failed to get recent feedback: %w", err)
	}
	stats.RecentFeedback = make([]RatingFeedback, 0, len(recentFeedback))
	for _, f := range recentFeedback {
		stats.RecentFeedback = append(stats.RecentFeedback, RatingFeedback{
			Rating:     f.Rating,
			Feedback:   f.Feedback,
			WasHelpful: f.WasHelpful,
			CreatedAt:  f.CreatedAt,
		})
	}
	return stats, nil
}

// GetTopRatedUsers returns the highest rated responders' public profiles.
func (s *FeedbackService) GetTopRatedUsers(limit int) ([]PublicRatedUser, error) {
	var users []models.User
	if err := s.db.Where("total_ratings >= ?", 5). // Minimum 5 ratings to be considered
		Order("average_rating DESC, total_ratings DESC").
		Limit(limit).
		Find(&users).Error; err != nil {
		return nil, fmt.Errorf("failed to get top rated users: %w", err)
	}

	public := make([]PublicRatedUser, 0, len(users))
	for _, u := range users {
		public = append(public, PublicRatedUser{
			ID:                u.ID,
			Name:              u.Name,
			ProfilePictureURL: u.ProfilePictureURL,
			AverageRating:     u.AverageRating,
			TotalRatings:      u.TotalRatings,
		})
	}
	return public, nil
}

type UserRatingStats struct {
	UserID         uint             `json:"user_id"`
	AverageRating  float64          `json:"average_rating"`
	TotalRatings   int              `json:"total_ratings"`
	FiveStarCount  int              `json:"five_star_count"`
	FourStarCount  int              `json:"four_star_count"`
	ThreeStarCount int              `json:"three_star_count"`
	TwoStarCount   int              `json:"two_star_count"`
	OneStarCount   int              `json:"one_star_count"`
	RecentFeedback []RatingFeedback `json:"recent_feedback,omitempty"`
}

// RatingFeedback is one rating as shown to the person who was rated. It
// leaves out the alert and who gave it, so it can't be traced to an emergency.
type RatingFeedback struct {
	Rating     int       `json:"rating"`
	Feedback   string    `json:"feedback,omitempty"`
	WasHelpful *bool     `json:"was_helpful,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// PublicRatedUser is what any signed-in user may see about a top responder:
// no phone number, email or activity history.
type PublicRatedUser struct {
	ID                uint    `json:"id"`
	Name              string  `json:"name"`
	ProfilePictureURL string  `json:"profile_picture_url,omitempty"`
	AverageRating     float64 `json:"average_rating"`
	TotalRatings      int     `json:"total_ratings"`
}