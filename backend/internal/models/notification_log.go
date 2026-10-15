package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// NotificationLog persists every outbound notification attempt for audit, retry, and analytics.
type NotificationLog struct {
	ID               uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID           uint           `gorm:"index;not null" json:"user_id"`
	NotificationType string         `gorm:"type:varchar(50);index;not null" json:"notification_type"` // emergency_alert, danger_zone, responder, etc.
	Title            string         `gorm:"type:varchar(255);not null" json:"title"`
	Body             string         `gorm:"type:text;not null" json:"body"`
	DataJSON         string         `gorm:"type:jsonb;column:data" json:"data,omitempty"` // encoded JSON payload
	Channel          string         `gorm:"type:varchar(20);not null" json:"channel"`     // expo, fcm, sms
	DeviceToken      string         `gorm:"type:text" json:"device_token,omitempty"`
	Status           string         `gorm:"type:varchar(20);index;default:'pending'" json:"status"` // pending, sent, delivered, failed, read
	ExternalID       string         `gorm:"type:varchar(255)" json:"external_id,omitempty"`         // FCM message ID or Twilio SID
	RetryCount       int            `gorm:"default:0" json:"retry_count"`
	SentAt           *time.Time     `gorm:"index" json:"sent_at,omitempty"`
	DeliveredAt      *time.Time     `json:"delivered_at,omitempty"`
	ReadAt           *time.Time     `json:"read_at,omitempty"`
	ErrorMessage     string         `gorm:"type:text" json:"error,omitempty"`
	AlertID          *uuid.UUID     `gorm:"type:uuid;index" json:"alert_id,omitempty"` // FK to emergency_alerts when applicable
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (NotificationLog) TableName() string {
	return "notification_logs"
}
