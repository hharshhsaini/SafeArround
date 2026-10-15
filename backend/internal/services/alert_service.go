package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/obsidiannnn/Safe-Around/backend/internal/models"
	customWS "github.com/obsidiannnn/Safe-Around/backend/internal/websocket"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type CreateAlertRequest struct {
	UserID     uint
	Location   models.Location
	AlertType  string
	SilentMode bool
	Metadata   string
}

type AlertService struct {
	db                  *gorm.DB
	redis               *redis.Client
	geofencingService   *GeofencingService
	notificationService NotificationService
	websocketHub        customWS.AlertBroadcaster
	schedulers          sync.Map // alert_id -> *time.Timer
}

const indiaEmergencyNumber = "112"

var (
	ErrAlertSelfResponse    = errors.New("requester cannot respond to their own alert")
	ErrAlertInactive        = errors.New("alert is no longer active")
	ErrAlertAlreadyAccepted = errors.New("responder has already accepted this alert")
	ErrAlertAccessDenied    = errors.New("you can only manage your own alert")
	ErrAlertNotDispatched   = errors.New("this alert was not sent to you")
)

type AlertResponderSummary struct {
	UserID         uint       `json:"user_id"`
	Name           string     `json:"name"`
	Phone          string     `json:"phone,omitempty"`
	ResponseStatus string     `json:"response_status"`
	DistanceMeters float64    `json:"distance_meters"`
	ETASeconds     int        `json:"eta_seconds"`
	RespondedAt    time.Time  `json:"responded_at"`
	ArrivedAt      *time.Time `json:"arrived_at,omitempty"`
	ResponseRating *int       `json:"response_rating,omitempty"`
}

type AlertIncidentReport struct {
	AlertID                 uuid.UUID `json:"alert_id"`
	RequesterUserID         uint      `json:"requester_user_id"`
	ResponderUserIDs        []uint    `json:"responder_user_ids"`
	Status                  string    `json:"status"`
	DurationSeconds         int       `json:"duration_seconds"`
	CreatedAt               time.Time `json:"created_at"`
	EndedAt                 time.Time `json:"ended_at"`
	CurrentRadius           int       `json:"current_radius"`
	MaxRadiusReached        int       `json:"max_radius_reached"`
	UsersNotified           int       `json:"users_notified"`
	RespondersCount         int64     `json:"responders_count"`
	EmergencyServicesStatus string    `json:"emergency_services_status"`
}

type AlertDetails struct {
	Alert                   models.EmergencyAlert       `json:"alert"`
	Timeline                []models.AlertTimelineEvent `json:"timeline"`
	RespondersCount         int64                       `json:"responders_count"`
	EmergencyNumber         string                      `json:"emergency_number"`
	DurationSeconds         int                         `json:"duration_seconds"`
	EmergencyServicesStatus string                      `json:"emergency_services_status"`
	Responders              []AlertResponderSummary     `json:"responders"`
	IncidentReport          AlertIncidentReport         `json:"incident_report"`
}

func NewAlertService(db *gorm.DB, rdb *redis.Client, gs *GeofencingService, ns NotificationService, wh customWS.AlertBroadcaster) *AlertService {
	return &AlertService{
		db:                  db,
		redis:               rdb,
		geofencingService:   gs,
		notificationService: ns,
		websocketHub:        wh,
	}
}

func (s *AlertService) GetAlertHistory(ctx context.Context, userID uint) ([]models.EmergencyAlert, error) {
	var alerts []models.EmergencyAlert
	if err := s.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&alerts).Error; err != nil {
		return nil, err
	}
	return alerts, nil
}

func (s *AlertService) GetOwnedAlert(ctx context.Context, alertID uuid.UUID, userID uint) (*models.EmergencyAlert, error) {
	var alert models.EmergencyAlert
	if err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", alertID, userID).
		First(&alert).Error; err != nil {
		return nil, err
	}

	return &alert, nil
}

// GetActiveAlerts returns the open alerts userID is involved in: their own,
// ones that reached them (dispatched, or as an emergency contact of the
// requester), and ones they are responding to.
func (s *AlertService) GetActiveAlerts(ctx context.Context, userID uint) ([]models.EmergencyAlert, error) {
	var alerts []models.EmergencyAlert
	if err := s.db.WithContext(ctx).Where("alert_status IN ?", []string{"active", "responding"}).Order("created_at DESC").Find(&alerts).Error; err != nil {
		return nil, err
	}
	if len(alerts) == 0 {
		return alerts, nil
	}

	alertIDs := make([]uuid.UUID, 0, len(alerts))
	for _, alert := range alerts {
		alertIDs = append(alertIDs, alert.ID)
	}

	dispatched, err := s.dispatchedUserIDs(ctx, alertIDs)
	if err != nil {
		return nil, err
	}
	contactOwners, err := s.emergencyContactOwnerIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	var respondingTo []uuid.UUID
	if err := s.db.WithContext(ctx).
		Model(&models.AlertResponse{}).
		Where("alert_id IN ? AND responder_user_id = ?", alertIDs, userID).
		Pluck("alert_id", &respondingTo).Error; err != nil {
		return nil, err
	}

	visible := make([]models.EmergencyAlert, 0, len(alerts))
	for _, alert := range alerts {
		_, isContact := contactOwners[alert.UserID]
		if alert.UserID == userID ||
			isContact ||
			slices.Contains(dispatched[alert.ID], userID) ||
			slices.Contains(respondingTo, alert.ID) {
			visible = append(visible, alert)
		}
	}
	return visible, nil
}

func (s *AlertService) GetAlertDetails(ctx context.Context, alertID uuid.UUID, userID uint) (*AlertDetails, error) {
	var alert models.EmergencyAlert
	if err := s.db.WithContext(ctx).
		Where(`
			id = ?
			AND (
				user_id = ?
				OR EXISTS (
					SELECT 1
					FROM alert_responses ar
					WHERE ar.alert_id = emergency_alerts.id
					  AND ar.responder_user_id = ?
					  AND ar.response_status IN ('accepted', 'arrived', 'helping')
				)
			)
		`, alertID, userID, userID).
		First(&alert).Error; err != nil {
		return nil, err
	}

	var timeline []models.AlertTimelineEvent
	if err := s.db.WithContext(ctx).
		Where("alert_id = ?", alertID).
		Order("occurred_at ASC").
		Find(&timeline).Error; err != nil {
		return nil, err
	}

	var respondersCount int64
	if err := s.db.WithContext(ctx).
		Model(&models.AlertResponse{}).
		Where("alert_id = ? AND response_status IN ?", alertID, []string{"accepted", "arrived", "helping"}).
		Count(&respondersCount).Error; err != nil {
		return nil, err
	}

	responders, err := s.getAlertResponders(ctx, alertID)
	if err != nil {
		return nil, err
	}

	emergencyServicesStatus, err := s.getEmergencyServicesStatus(ctx, alertID)
	if err != nil {
		return nil, err
	}

	durationSeconds := s.calculateAlertDuration(alert)
	reportEndedAt := alert.CreatedAt.Add(time.Duration(durationSeconds) * time.Second)
	if alert.ResolvedAt != nil {
		reportEndedAt = *alert.ResolvedAt
	} else if alert.CancelledAt != nil {
		reportEndedAt = *alert.CancelledAt
	}

	return &AlertDetails{
		Alert:                   alert,
		Timeline:                timeline,
		RespondersCount:         respondersCount,
		EmergencyNumber:         indiaEmergencyNumber,
		DurationSeconds:         durationSeconds,
		EmergencyServicesStatus: emergencyServicesStatus,
		Responders:              responders,
		IncidentReport: AlertIncidentReport{
			AlertID:                 alert.ID,
			RequesterUserID:         alert.UserID,
			ResponderUserIDs:        extractResponderIDs(responders),
			Status:                  alert.AlertStatus,
			DurationSeconds:         durationSeconds,
			CreatedAt:               alert.CreatedAt,
			EndedAt:                 reportEndedAt,
			CurrentRadius:           alert.CurrentRadius,
			MaxRadiusReached:        alert.MaxRadiusReached,
			UsersNotified:           alert.UsersNotified,
			RespondersCount:         respondersCount,
			EmergencyServicesStatus: emergencyServicesStatus,
		},
	}, nil
}

func extractResponderIDs(responders []AlertResponderSummary) []uint {
	ids := make([]uint, 0, len(responders))
	for _, responder := range responders {
		ids = append(ids, responder.UserID)
	}
	return ids
}

func (s *AlertService) CreateAlert(ctx context.Context, req CreateAlertRequest) (*models.EmergencyAlert, error) {
	var metadata *string
	if req.Metadata != "" {
		if !json.Valid([]byte(req.Metadata)) {
			wrapped, err := json.Marshal(map[string]string{"message": req.Metadata})
			if err != nil {
				return nil, err
			}
			metadataStr := string(wrapped)
			metadata = &metadataStr
		} else {
			metadata = &req.Metadata
		}
	}

	// 1. Create alert record
	alert := &models.EmergencyAlert{
		UserID:        req.UserID,
		AlertLocation: req.Location,
		AlertType:     req.AlertType,
		SilentMode:    req.SilentMode,
		Metadata:      metadata,
		CurrentRadius: 100,
	}

	if err := s.db.Create(alert).Error; err != nil {
		return nil, err
	}
	s.db.Model(&models.User{}).
		Where("id = ?", req.UserID).
		UpdateColumn("total_alerts_triggered", gorm.Expr("total_alerts_triggered + ?", 1))

	// 2. Find nearby users (100m)
	nearbyUsers, err := s.geofencingService.GetDispatchCandidates(
		req.Location.Latitude,
		req.Location.Longitude,
		100,
	)
	if err != nil {
		return nil, err
	}

	// 3. Send notifications
	usersNotified := 0
	recipientIDs := make([]uint, 0, len(nearbyUsers))
	for _, user := range nearbyUsers {
		if user.ID == req.UserID {
			continue
		}
		s.notificationService.SendEmergencyAlert(user.ID, alert)
		recipientIDs = append(recipientIDs, user.ID)
		usersNotified++
	}
	alert.UsersNotified = usersNotified
	s.db.Model(alert).Update("users_notified", usersNotified)

	// 4. Log timeline event
	s.logTimelineEvent(alert.ID, "created", 100, usersNotified, 0, map[string]interface{}{
		"requester_user_id":  req.UserID,
		"recipient_user_ids": recipientIDs,
	})

	// 5. Start radius expansion scheduler
	s.startRadiusExpansion(alert.ID)

	// 6. Broadcast via WebSocket
	if s.websocketHub != nil {
		s.websocketHub.BroadcastEmergencyAlert(alert, recipientIDs)
	}

	// 7. Notify emergency contacts immediately
	s.notificationService.NotifyEmergencyContacts(alert.UserID, alert)

	return alert, nil
}

func (s *AlertService) startRadiusExpansion(alertID uuid.UUID) {
	// Start a ticker-like background job for granular expansion
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				var alert models.EmergencyAlert
				if err := s.db.First(&alert, "id = ?", alertID).Error; err != nil {
					return
				}

				// Stop expansion if not active or already has responders
				if alert.AlertStatus != "active" {
					return
				}

				var responderCount int64
				s.db.Model(&models.AlertResponse{}).
					Where("alert_id = ? AND response_status = ?", alertID, "accepted").
					Count(&responderCount)

				if responderCount > 0 {
					return
				}

				nextRadius := alert.GetNextRadius()
				if nextRadius == alert.CurrentRadius {
					// Max radius reached or couldn't get next
					return
				}

				s.expandRadius(alertID, nextRadius)
			}
		}
	}()
}

func (s *AlertService) cancelRadiusExpansion(alertID uuid.UUID) {
	for _, suffix := range []string{":30", ":60", ":90"} {
		key := alertID.String() + suffix
		if timer, ok := s.schedulers.Load(key); ok {
			timer.(*time.Timer).Stop()
			s.schedulers.Delete(key)
		}
	}
}

func (s *AlertService) expandRadius(alertID uuid.UUID, newRadius int) error {
	var alert models.EmergencyAlert
	if err := s.db.First(&alert, "id = ?", alertID).Error; err != nil {
		return err
	}

	// Don't expand if already resolved/cancelled
	if alert.AlertStatus != "active" && alert.AlertStatus != "responding" {
		return nil
	}

	// Don't expand if already at this radius
	if alert.CurrentRadius >= newRadius {
		return nil
	}

	// Check if we have responders
	var responderCount int64
	s.db.Model(&models.AlertResponse{}).
		Where("alert_id = ? AND response_status = ?", alertID, "accepted").
		Count(&responderCount)

	// If we have responders, don't expand
	if responderCount > 0 {
		return nil
	}

	// Find new users in expanded radius
	oldRadius := alert.CurrentRadius
	newUsers, err := s.geofencingService.GetDispatchCandidatesInRing(
		alert.AlertLocation.Latitude,
		alert.AlertLocation.Longitude,
		oldRadius,
		newRadius,
	)
	if err != nil {
		return err
	}

	// Send notifications to new users
	recipientIDs := make([]uint, 0, len(newUsers))
	for _, user := range newUsers {
		if user.ID == alert.UserID {
			continue
		}
		s.notificationService.SendEmergencyAlert(user.ID, &alert)
		recipientIDs = append(recipientIDs, user.ID)
	}

	// Update alert
	alert.CurrentRadius = newRadius
	alert.MaxRadiusReached = newRadius
	alert.UsersNotified += len(newUsers)
	s.db.Save(&alert)

	// Log timeline
	s.logTimelineEvent(alertID, "radius_expanded", newRadius, alert.UsersNotified, int(responderCount), map[string]interface{}{
		"requester_user_id": alert.UserID,
		"old_radius":        oldRadius,
		"new_radius":        newRadius,
		"new_user_ids":      recipientIDs,
	})

	// Broadcast via WebSocket
	if s.websocketHub != nil {
		s.websocketHub.BroadcastRadiusExpanded(alertID, oldRadius, newRadius, alert.UsersNotified, recipientIDs)
	}

	return nil
}

func (s *AlertService) AcceptAlert(ctx context.Context, alertID uuid.UUID, responderID uint, location models.Location) error {
	var alert models.EmergencyAlert
	if err := s.db.First(&alert, "id = ?", alertID).Error; err != nil {
		return err
	}

	if alert.UserID == responderID {
		return ErrAlertSelfResponse
	}

	if alert.AlertStatus != "active" && alert.AlertStatus != "responding" {
		return ErrAlertInactive
	}

	askedToHelp, err := s.wasAskedToHelp(ctx, alert, responderID)
	if err != nil {
		return err
	}
	if !askedToHelp {
		return ErrAlertNotDispatched
	}

	var existingResponse models.AlertResponse
	if err := s.db.Where("alert_id = ? AND responder_user_id = ? AND response_status IN ?", alertID, responderID, []string{"accepted", "arrived", "helping"}).
		First(&existingResponse).Error; err == nil {
		return ErrAlertAlreadyAccepted
	}

	// Create response
	response := &models.AlertResponse{
		AlertID:           alertID,
		ResponderUserID:   responderID,
		ResponseStatus:    "accepted",
		ResponderLocation: location,
	}

	// Calculate distance and ETA
	response.DistanceMeters = responderDistanceMeters(location, alert.AlertLocation)
	if err := response.CalculateDistance(); err != nil {
		return err
	}
	response.EstimateETA()

	if err := s.db.Create(response).Error; err != nil {
		return err
	}

	// Update alert status
	s.db.Model(&models.EmergencyAlert{}).
		Where("id = ?", alertID).
		Update("alert_status", "responding")

	// Cancel radius expansion if we have responders
	s.cancelRadiusExpansion(alertID)

	// Notify victim
	s.notificationService.NotifyResponderAccepted(alert.UserID, response)

	var respondersCount int64
	s.db.Model(&models.AlertResponse{}).
		Where("alert_id = ? AND response_status = ?", alertID, "accepted").
		Count(&respondersCount)

	// Broadcast via WebSocket
	if s.websocketHub != nil {
		s.websocketHub.BroadcastResponderAccepted(alertID, response, alert.UserID)
	}

	s.logTimelineEvent(alertID, "responder_accepted", alert.CurrentRadius, alert.UsersNotified, int(respondersCount), map[string]interface{}{
		"requester_user_id": alert.UserID,
		"responder_user_id": responderID,
		"response_id":       response.ID.String(),
	})

	return nil
}

func (s *AlertService) ResolveAlert(ctx context.Context, alertID uuid.UUID, resolvedBy uint, resolutionType string) error {
	var alert models.EmergencyAlert
	if err := s.db.WithContext(ctx).First(&alert, "id = ?", alertID).Error; err != nil {
		return err
	}
	if alert.UserID != resolvedBy {
		return ErrAlertAccessDenied
	}
	if alert.AlertStatus == "resolved" {
		return nil
	}
	if alert.AlertStatus != "active" && alert.AlertStatus != "responding" {
		return ErrAlertInactive
	}

	now := time.Now()
	if err := s.db.Model(&models.EmergencyAlert{}).
		Where("id = ?", alertID).
		Updates(map[string]interface{}{
			"alert_status": "resolved",
			"resolved_at":  now,
		}).Error; err != nil {
		return err
	}

	// Credit responders only once the requester marks the alert resolved, not
	// when they accept; this runs once because a resolved alert returns early.
	var responderIDs []uint
	s.db.WithContext(ctx).
		Model(&models.AlertResponse{}).
		Where("alert_id = ? AND response_status IN ?", alertID, []string{"accepted", "arrived", "helping"}).
		Pluck("responder_user_id", &responderIDs)
	if len(responderIDs) > 0 {
		s.db.WithContext(ctx).
			Model(&models.User{}).
			Where("id IN ?", responderIDs).
			UpdateColumn("people_helped_count", gorm.Expr("people_helped_count + ?", 1))
	}

	// Cancel any pending expansions
	s.cancelRadiusExpansion(alertID)

	// Log timeline
	s.logTimelineEvent(alertID, "resolved", 0, 0, 0, map[string]interface{}{
		"requester_user_id": resolvedBy,
		"resolution_type":   resolutionType,
	})

	// Notify all participants
	s.notificationService.NotifyAllParticipants(alertID, fmt.Sprintf("Alert resolved: %s", resolutionType))

	// Close WebSocket room
	if s.websocketHub != nil {
		s.websocketHub.CloseRoom("alert_"+alertID.String(), "resolved", s.alertParticipantIDs(ctx, alert))
	}

	return nil
}

func (s *AlertService) CancelAlert(ctx context.Context, alertID uuid.UUID, cancelledBy uint) error {
	var alert models.EmergencyAlert
	if err := s.db.WithContext(ctx).First(&alert, "id = ?", alertID).Error; err != nil {
		return err
	}
	if alert.UserID != cancelledBy {
		return ErrAlertAccessDenied
	}
	if alert.AlertStatus == "cancelled" {
		return nil
	}
	if alert.AlertStatus != "active" && alert.AlertStatus != "responding" {
		return ErrAlertInactive
	}

	now := time.Now()
	if err := s.db.Model(&models.EmergencyAlert{}).
		Where("id = ? AND user_id = ?", alertID, cancelledBy).
		Updates(map[string]interface{}{
			"alert_status": "cancelled",
			"cancelled_at": now,
		}).Error; err != nil {
		return err
	}

	s.cancelRadiusExpansion(alertID)
	s.logTimelineEvent(alertID, "cancelled", 0, 0, 0, map[string]interface{}{
		"requester_user_id": cancelledBy,
	})
	s.notificationService.NotifyAllParticipants(alertID, "Alert cancelled by the requester")

	if s.websocketHub != nil {
		s.websocketHub.CloseRoom("alert_"+alertID.String(), "cancelled", s.alertParticipantIDs(ctx, alert))
	}

	return nil
}

func (s *AlertService) EscalateToEmergencyServices(alertID uuid.UUID, requestedBy uint, escalationType string) error {
	var alert models.EmergencyAlert
	if err := s.db.First(&alert, "id = ?", alertID).Error; err != nil {
		return err
	}

	// Only the requester or someone actively responding may call in emergency services.
	if alert.UserID != requestedBy {
		var activeResponses int64
		if err := s.db.Model(&models.AlertResponse{}).
			Where("alert_id = ? AND responder_user_id = ? AND response_status IN ?", alertID, requestedBy, []string{"accepted", "arrived", "helping"}).
			Count(&activeResponses).Error; err != nil {
			return err
		}
		if activeResponses == 0 {
			return ErrAlertAccessDenied
		}
	}

	// SafeAround cannot reach emergency services itself, so this records the
	// request (and alerts emergency contacts) without claiming anyone was called.
	payload, err := json.Marshal(map[string]interface{}{
		"location": map[string]float64{
			"latitude":  alert.AlertLocation.Latitude,
			"longitude": alert.AlertLocation.Longitude,
		},
		"caller_info":    s.getUserInfo(alert.UserID),
		"situation_type": alert.AlertType,
	})
	if err != nil {
		return err
	}

	escalation := &models.AlertEscalation{
		AlertID:           alertID,
		EscalationType:    escalationType,
		EscalationStatus:  "requested",
		EscalationPayload: string(payload),
	}
	if err := s.db.Create(escalation).Error; err != nil {
		return err
	}

	// Notify emergency contacts
	s.notificationService.NotifyEmergencyContacts(alert.UserID, &alert)

	// Log timeline
	s.logTimelineEvent(alertID, "escalated", 0, 0, 0, map[string]interface{}{
		"requester_user_id": alert.UserID,
		"escalation_type":   escalationType,
	})

	return nil
}

// Helpers

func (s *AlertService) logTimelineEvent(alertID uuid.UUID, eventType string, radius, usersNotified, respondersCount int, eventData ...map[string]interface{}) {
	payload := ""
	if len(eventData) > 0 && eventData[0] != nil {
		if raw, err := json.Marshal(eventData[0]); err == nil {
			payload = string(raw)
		}
	}

	event := models.AlertTimelineEvent{
		AlertID:         alertID,
		EventType:       eventType,
		RadiusAtEvent:   radius,
		UsersNotified:   usersNotified,
		RespondersCount: respondersCount,
		EventData:       payload,
	}
	s.db.Create(&event)
}

// alertParticipantIDs returns everyone who was told about an alert: the
// requester, each user it was dispatched to (initially and on every radius
// expansion), the requester's registered emergency contacts, and each responder.
func (s *AlertService) alertParticipantIDs(ctx context.Context, alert models.EmergencyAlert) []uint {
	seen := map[uint]struct{}{}
	ids := make([]uint, 0)
	add := func(id uint) {
		if _, ok := seen[id]; ok || id == 0 {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	add(alert.UserID)

	dispatched, _ := s.dispatchedUserIDs(ctx, []uuid.UUID{alert.ID})
	for _, id := range dispatched[alert.ID] {
		add(id)
	}

	var contactUserIDs []uint
	s.db.WithContext(ctx).
		Model(&models.User{}).
		Joins("JOIN emergency_contacts ON emergency_contacts.phone = users.phone AND emergency_contacts.deleted_at IS NULL").
		Where("emergency_contacts.user_id = ?", alert.UserID).
		Pluck("users.id", &contactUserIDs)
	for _, id := range contactUserIDs {
		add(id)
	}

	var responderIDs []uint
	s.db.WithContext(ctx).
		Model(&models.AlertResponse{}).
		Where("alert_id = ?", alert.ID).
		Pluck("responder_user_id", &responderIDs)
	for _, id := range responderIDs {
		add(id)
	}

	return ids
}

// dispatchedUserIDs returns, for each alert, the users it was dispatched to,
// initially and on every radius expansion.
func (s *AlertService) dispatchedUserIDs(ctx context.Context, alertIDs []uuid.UUID) (map[uuid.UUID][]uint, error) {
	dispatched := make(map[uuid.UUID][]uint, len(alertIDs))
	if len(alertIDs) == 0 {
		return dispatched, nil
	}

	var events []models.AlertTimelineEvent
	if err := s.db.WithContext(ctx).
		Where("alert_id IN ? AND event_type IN ?", alertIDs, []string{"created", "radius_expanded"}).
		Find(&events).Error; err != nil {
		return nil, err
	}
	for _, event := range events {
		var data struct {
			RecipientUserIDs []uint `json:"recipient_user_ids"`
			NewUserIDs       []uint `json:"new_user_ids"`
		}
		if err := json.Unmarshal([]byte(event.EventData), &data); err != nil {
			continue
		}
		dispatched[event.AlertID] = append(dispatched[event.AlertID], data.RecipientUserIDs...)
		dispatched[event.AlertID] = append(dispatched[event.AlertID], data.NewUserIDs...)
	}
	return dispatched, nil
}

// emergencyContactOwnerIDs returns the users who saved userID's phone number
// as an emergency contact. Their alerts are pushed straight to userID.
func (s *AlertService) emergencyContactOwnerIDs(ctx context.Context, userID uint) (map[uint]struct{}, error) {
	var ownerIDs []uint
	if err := s.db.WithContext(ctx).
		Model(&models.EmergencyContact{}).
		Joins("JOIN users ON users.phone = emergency_contacts.phone AND users.deleted_at IS NULL").
		Where("users.id = ?", userID).
		Pluck("emergency_contacts.user_id", &ownerIDs).Error; err != nil {
		return nil, err
	}

	owners := make(map[uint]struct{}, len(ownerIDs))
	for _, id := range ownerIDs {
		owners[id] = struct{}{}
	}
	return owners, nil
}

// wasAskedToHelp reports whether an alert reached userID, either because it
// was dispatched to them or because they are the requester's emergency contact.
func (s *AlertService) wasAskedToHelp(ctx context.Context, alert models.EmergencyAlert, userID uint) (bool, error) {
	dispatched, err := s.dispatchedUserIDs(ctx, []uuid.UUID{alert.ID})
	if err != nil {
		return false, err
	}
	if slices.Contains(dispatched[alert.ID], userID) {
		return true, nil
	}

	contactOwners, err := s.emergencyContactOwnerIDs(ctx, userID)
	if err != nil {
		return false, err
	}
	_, isContact := contactOwners[alert.UserID]
	return isContact, nil
}

func (s *AlertService) getUserInfo(userID uint) string {
	var user models.User
	s.db.Select("name", "phone").First(&user, userID)
	return fmt.Sprintf("Name: %s, Phone: %s", user.Name, user.Phone)
}

func (s *AlertService) calculateAlertDuration(alert models.EmergencyAlert) int {
	endTime := time.Now()
	if alert.ResolvedAt != nil {
		endTime = *alert.ResolvedAt
	} else if alert.CancelledAt != nil {
		endTime = *alert.CancelledAt
	}

	if endTime.Before(alert.CreatedAt) {
		return 0
	}

	return int(endTime.Sub(alert.CreatedAt).Seconds())
}

func (s *AlertService) getEmergencyServicesStatus(ctx context.Context, alertID uuid.UUID) (string, error) {
	var escalation models.AlertEscalation
	err := s.db.WithContext(ctx).
		Where("alert_id = ?", alertID).
		Order("escalated_at DESC").
		First(&escalation).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "Not contacted", nil
	}
	if err != nil {
		return "", err
	}

	// No escalation ever reached emergency services: the old "dispatched"
	// records came from a mock call, and new ones are only requests.
	return fmt.Sprintf("%s requested; not contacted automatically, call %s", escalation.EscalationType, indiaEmergencyNumber), nil
}

func (s *AlertService) getAlertResponders(ctx context.Context, alertID uuid.UUID) ([]AlertResponderSummary, error) {
	type responderRow struct {
		UserID         uint       `gorm:"column:user_id"`
		Name           string     `gorm:"column:name"`
		Phone          string     `gorm:"column:phone"`
		ResponseStatus string     `gorm:"column:response_status"`
		DistanceMeters float64    `gorm:"column:distance_meters"`
		ETASeconds     int        `gorm:"column:eta_seconds"`
		RespondedAt    time.Time  `gorm:"column:responded_at"`
		ArrivedAt      *time.Time `gorm:"column:arrived_at"`
		ResponseRating *int       `gorm:"column:response_rating"`
	}

	var rows []responderRow
	err := s.db.WithContext(ctx).Raw(`
		SELECT
			u.id AS user_id,
			COALESCE(NULLIF(u.name, ''), 'Verified responder') AS name,
			u.phone AS phone,
			ar.response_status,
			COALESCE(ar.distance_meters, 0) AS distance_meters,
			GREATEST(COALESCE(ar.estimated_arrival_minutes, 0), 0) * 60 AS eta_seconds,
			ar.responded_at,
			ar.arrived_at,
			ar.response_rating
		FROM alert_responses ar
		JOIN users u ON u.id = ar.responder_user_id
		WHERE ar.alert_id = ?
		  AND ar.response_status IN ('accepted', 'arrived', 'helping')
		ORDER BY ar.responded_at ASC
	`, alertID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	responders := make([]AlertResponderSummary, 0, len(rows))
	for _, row := range rows {
		responders = append(responders, AlertResponderSummary{
			UserID:         row.UserID,
			Name:           row.Name,
			Phone:          row.Phone,
			ResponseStatus: row.ResponseStatus,
			DistanceMeters: row.DistanceMeters,
			ETASeconds:     row.ETASeconds,
			RespondedAt:    row.RespondedAt,
			ArrivedAt:      row.ArrivedAt,
			ResponseRating: row.ResponseRating,
		})
	}

	return responders, nil
}

func responderDistanceMeters(a, b models.Location) float64 {
	const earthRadius = 6371000.0
	lat1 := a.Latitude * math.Pi / 180
	lat2 := b.Latitude * math.Pi / 180
	dLat := (b.Latitude - a.Latitude) * math.Pi / 180
	dLng := (b.Longitude - a.Longitude) * math.Pi / 180
	sinLat := math.Sin(dLat / 2)
	sinLng := math.Sin(dLng / 2)
	h := sinLat*sinLat + math.Cos(lat1)*math.Cos(lat2)*sinLng*sinLng
	return earthRadius * 2 * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))
}
