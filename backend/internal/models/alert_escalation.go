package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AlertEscalation manages external emergency services escalation from standard SOS calls.
type AlertEscalation struct {
	ID                  uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	AlertID             uuid.UUID  `gorm:"type:uuid;not null;index" json:"alert_id"`
	EscalationType      string     `gorm:"type:varchar(50);not null" json:"escalation_type"` // police, ambulance, fire, all
	EscalationStatus    string     `gorm:"type:varchar(50);default:'pending'" json:"escalation_status"` // requested (legacy rows: pending, dispatched from a mock call)
	EscalationPayload   string     `gorm:"type:jsonb" json:"escalation_payload"` // Stored as JSONB string representing NENA i3 standard
	ExternalReferenceID string     `gorm:"type:varchar(255)" json:"external_reference_id"` // E.g., Twilio emergency call SID
	EscalatedAt         time.Time  `gorm:"autoCreateTime" json:"escalated_at"`
	AcknowledgedAt      *time.Time `json:"acknowledged_at,omitempty"`
}

func (e *AlertEscalation) TableName() string {
	return "alert_escalations"
}

func (e *AlertEscalation) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}
