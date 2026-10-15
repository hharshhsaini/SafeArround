package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/obsidiannnn/Safe-Around/backend/internal/models"
	"github.com/obsidiannnn/Safe-Around/backend/internal/utils"
	"github.com/obsidiannnn/Safe-Around/backend/pkg/fcm"
	"github.com/obsidiannnn/Safe-Around/backend/pkg/logger"
	"github.com/obsidiannnn/Safe-Around/backend/pkg/twilio"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	notificationCategoryEmergencyAlert = "EMERGENCY_ALERT"
	notificationCategoryAlertStatus    = "ALERT_STATUS"
)

// --- Interface ---

// ErrNotificationNotFound means no notification with that ID belongs to the user.
var ErrNotificationNotFound = errors.New("notification not found")

type NotificationService interface {
	SendSMS(phone, message string) error
	QueueNotification(userID uint, title, body, notifType string) error
	GetHistory(userID uint, limit, offset int) ([]models.NotificationLog, int64, error)
	MarkAsRead(userID, notifID uint) error
	RegisterDeviceToken(userID uint, token, platform string) error
	SendEmergencyAlert(userID uint, alert *models.EmergencyAlert) error
	NotifyResponderAccepted(userID uint, response *models.AlertResponse) error
	NotifyAllParticipants(alertID uuid.UUID, message string) error
	NotifyEmergencyContacts(userID uint, alert *models.EmergencyAlert) error
}

// --- Internal Task struct ---

type notificationTask struct {
	UserID           uint
	NotificationType string
	Title            string
	Body             string
	Data             map[string]interface{}
	Priority         string // "critical", "high", "normal"
	AlertID          *uuid.UUID
	Phone            string // Override: send SMS directly to this phone
}

// --- Service Implementation ---

type notifService struct {
	fcm    *fcm.Client
	twilio *twilio.Client
	db     *gorm.DB
	redis  *redis.Client
	queue  chan *notificationTask
}

func NewNotificationService(fcmClient *fcm.Client, twilioClient *twilio.Client, db *gorm.DB, rdb *redis.Client) NotificationService {
	ns := &notifService{
		fcm:    fcmClient,
		twilio: twilioClient,
		db:     db,
		redis:  rdb,
		queue:  make(chan *notificationTask, 1000),
	}
	// Start 10-worker goroutine pool
	for i := 0; i < 10; i++ {
		go ns.worker()
	}
	return ns
}

// worker processes notifications from the queue
func (s *notifService) worker() {
	for task := range s.queue {
		s.processTask(task)
	}
}

// ---- Public API ----

func (s *notifService) SendSMS(phone, message string) error {
	if phone == "" {
		return nil
	}
	fromNum := os.Getenv("TWILIO_FROM")
	_, err := s.twilio.SendSMS(phone, fromNum, message)
	if err != nil {
		logger.Warn("SMS send failed", zap.String("phone", phone), zap.Error(err))
	}
	return err
}

func (s *notifService) QueueNotification(userID uint, title, body, notifType string) error {
	ctx := context.Background()
	payload := map[string]interface{}{
		"user_id": userID,
		"title":   title,
		"body":    body,
		"type":    notifType,
	}
	return s.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: "notification_queue",
		Values: payload,
	}).Err()
}

// GetHistory lists the notifications sent to a user. They live in
// notification_logs, the same table MarkAsRead updates.
func (s *notifService) GetHistory(userID uint, limit, offset int) ([]models.NotificationLog, int64, error) {
	var notifs []models.NotificationLog
	var total int64
	query := s.db.Model(&models.NotificationLog{}).Where("user_id = ?", userID)
	query.Count(&total)
	err := query.Order("created_at desc").Limit(limit).Offset(offset).Find(&notifs).Error
	return notifs, total, err
}

// MarkAsRead marks one of the user's own notifications as read.
func (s *notifService) MarkAsRead(userID, notifID uint) error {
	now := time.Now()
	result := s.db.Model(&models.NotificationLog{}).
		Where("id = ? AND user_id = ?", notifID, userID).
		Updates(map[string]interface{}{"status": "read", "read_at": &now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotificationNotFound
	}
	return nil
}

func (s *notifService) RegisterDeviceToken(userID uint, token, platform string) error {
	token = strings.TrimSpace(token)
	platform = strings.ToLower(strings.TrimSpace(platform))
	if token == "" {
		return errors.New("device token is required")
	}

	var session models.UserSession
	err := s.db.
		Where("user_id = ? AND is_revoked = ?", userID, false).
		Order("created_at DESC").
		First(&session).Error
	if err != nil {
		return err
	}

	if platform == "ios" {
		session.APNsToken = token
		session.FCMToken = ""
	} else {
		session.FCMToken = token
		session.APNsToken = ""
	}

	return s.db.Save(&session).Error
}

// ---- Emergency-specific methods ----

func (s *notifService) SendEmergencyAlert(userID uint, alert *models.EmergencyAlert) error {
	task := &notificationTask{
		UserID:           userID,
		NotificationType: "emergency_alert",
		Title:            "🚨 Emergency Alert Nearby",
		Body:             "Someone near you needs urgent help. Please respond.",
		Data: map[string]interface{}{
			"alert_id":       alert.ID.String(),
			"alert_type":     alert.AlertType,
			"category":       notificationCategoryEmergencyAlert,
			"user_id":        alert.UserID,
			"latitude":       alert.AlertLocation.Latitude,
			"longitude":      alert.AlertLocation.Longitude,
			"current_radius": alert.CurrentRadius,
			"users_notified": alert.UsersNotified,
			"created_at":     alert.CreatedAt.UTC().Format(time.RFC3339),
		},
		Priority: "critical",
		AlertID:  &alert.ID,
	}
	// Critical: try queue, fall back to direct
	select {
	case s.queue <- task:
	default:
		go s.processTask(task)
	}
	return nil
}

func (s *notifService) NotifyResponderAccepted(userID uint, response *models.AlertResponse) error {
	task := &notificationTask{
		UserID:           userID,
		NotificationType: "responder_accepted",
		Title:            "✅ Help is on the way!",
		Body:             fmt.Sprintf("A responder is %d meters away (~%d min ETA)", int(response.DistanceMeters), response.EstimatedArrivalMinutes),
		Data: map[string]interface{}{
			"alert_id":    response.AlertID,
			"category":    notificationCategoryAlertStatus,
			"distance_m":  response.DistanceMeters,
			"eta_minutes": response.EstimatedArrivalMinutes,
		},
		Priority: "high",
		AlertID:  &response.AlertID,
	}
	s.queue <- task
	return nil
}

func (s *notifService) NotifyAllParticipants(alertID uuid.UUID, message string) error {
	// Find all responders for this alert
	var responses []models.AlertResponse
	s.db.Where("alert_id = ?", alertID).Find(&responses)

	for _, r := range responses {
		s.queue <- &notificationTask{
			UserID:           r.ResponderUserID,
			NotificationType: "alert_update",
			Title:            "Alert Update",
			Body:             message,
			Data: map[string]interface{}{
				"alert_id": alertID.String(),
				"category": notificationCategoryAlertStatus,
				"room_id":  fmt.Sprintf("alert_%s", alertID.String()),
				"message":  message,
			},
			Priority: "high",
			AlertID:  &alertID,
		}
	}
	return nil
}

func (s *notifService) NotifyEmergencyContacts(userID uint, alert *models.EmergencyAlert) error {
	var contacts []models.EmergencyContact
	s.db.Where("user_id = ?", userID).Find(&contacts)

	var user models.User
	s.db.First(&user, userID)

	msgText := fmt.Sprintf(
		"EMERGENCY: %s has triggered an SOS alert. Location: https://maps.google.com/?q=%f,%f — Please check on them immediately.",
		user.Name,
		alert.AlertLocation.Latitude,
		alert.AlertLocation.Longitude,
	)

	for _, contact := range contacts {
		// Contacts saved before numbers were normalised may lack the +91, which
		// would make both the user lookup and the SMS fail.
		phone := utils.NormalizePhone(contact.Phone)

		// 1. Try to find a registered user with this phone number
		var contactUser models.User
		if err := s.db.Where("phone = ?", phone).First(&contactUser).Error; err == nil {
			// Found a registered user! Send in-app notification
			s.queue <- &notificationTask{
				UserID:           contactUser.ID,
				NotificationType: "emergency_contact_alert",
				Title:            "🚨 Emergency: " + user.Name,
				Body:             user.Name + " is in danger! Tap to see live location.",
				Data: map[string]interface{}{
					"alert_id":       alert.ID.String(),
					"category":       notificationCategoryEmergencyAlert,
					"user_id":        alert.UserID,
					"latitude":       alert.AlertLocation.Latitude,
					"longitude":      alert.AlertLocation.Longitude,
					"current_radius": alert.CurrentRadius,
					"users_notified": alert.UsersNotified,
					"created_at":     alert.CreatedAt.UTC().Format(time.RFC3339),
					"victim_name":    user.Name,
					"victim_phone":   user.Phone,
				},
				Priority: "critical",
				AlertID:  &alert.ID,
			}
		}

		// 2. Always send SMS backup (as requested: "message will be triggered")
		go func(phone string) {
			if err := s.SendSMS(phone, msgText); err != nil {
				logger.Warn("Emergency contact SMS/Call failed", zap.String("phone", phone), zap.Error(err))
			}
		}(phone)
	}
	return nil
}

// ---- Internal Processing ----

func (s *notifService) processTask(task *notificationTask) {
	// Look up the user's active session for device token
	var session models.UserSession
	sessionErr := s.db.Where("user_id = ? AND is_revoked = ?", task.UserID, false).
		Order("created_at DESC").
		First(&session).Error

	dataJSON, _ := json.Marshal(task.Data)
	logEntry := &models.NotificationLog{
		UserID:           task.UserID,
		NotificationType: task.NotificationType,
		Title:            task.Title,
		Body:             task.Body,
		DataJSON:         string(dataJSON),
		Status:           "pending",
		AlertID:          task.AlertID,
	}

	if sessionErr != nil || (session.FCMToken == "" && session.APNsToken == "") {
		// No device token found — mark as failed
		logEntry.Status = "failed"
		logEntry.ErrorMessage = "no active device token found"
		s.db.Create(logEntry)

		// For critical: SMS fallback
		if task.Priority == "critical" {
			s.smsFallback(task)
		}
		return
	}

	strData := make(map[string]string)
	for k, v := range task.Data {
		strData[k] = fmt.Sprint(v)
	}

	// The app registers Expo push tokens on both platforms (iOS ones are stored
	// in the APNs column), and the client sends each token the right way.
	deviceToken := session.FCMToken
	if deviceToken == "" {
		deviceToken = session.APNsToken
	}
	logEntry.Channel = "fcm"
	if fcm.IsExpoPushToken(deviceToken) {
		logEntry.Channel = "expo"
	}
	logEntry.DeviceToken = deviceToken
	msgID, sendErr := s.sendWithRetry(deviceToken, task.Title, task.Body, strData)

	now := time.Now()
	if sendErr != nil {
		logEntry.Status = "failed"
		logEntry.ErrorMessage = sendErr.Error()
	} else {
		logEntry.Status = "sent"
		logEntry.ExternalID = msgID
		logEntry.SentAt = &now
	}

	s.db.Create(logEntry)

	// Critical: SMS only when the push could not be sent
	if sendErr != nil && task.Priority == "critical" {
		s.smsFallback(task)
	}
}

// sendWithRetry sends a push, retrying after 1s and 5s. Failures that a retry
// can't fix return at once so the SMS fallback isn't held up.
func (s *notifService) sendWithRetry(token, title, body string, data map[string]string) (string, error) {
	delays := []time.Duration{1 * time.Second, 5 * time.Second}
	for attempt := 0; ; attempt++ {
		msgID, err := s.fcm.SendNotification(token, title, body, data)
		if err == nil {
			return msgID, nil
		}
		if errors.Is(err, fcm.ErrUndeliverable) || attempt == len(delays) {
			return "", err
		}
		logger.Warn("Push retry", zap.Int("attempt", attempt+1), zap.Error(err))
		time.Sleep(delays[attempt])
	}
}

// smsFallback sends an SMS via Twilio when push fails
func (s *notifService) smsFallback(task *notificationTask) {
	var user models.User
	if err := s.db.First(&user, task.UserID).Error; err != nil || user.Phone == "" {
		return
	}
	if err := s.SendSMS(user.Phone, task.Body); err != nil {
		logger.Warn("SMS fallback failed", zap.Uint("userID", task.UserID), zap.Error(err))
	}
}
