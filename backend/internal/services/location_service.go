package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/obsidiannnn/Safe-Around/backend/internal/models"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type LocationService struct {
	db                *gorm.DB
	redis             *redis.Client
	geofencingService *GeofencingService
	locationChan      chan *models.UserLocation
	workerPool        int
}

type NearbyUserLocation struct {
	UserID     uint      `json:"user_id"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	RecordedAt time.Time `json:"recorded_at"`
}

// NearbyUserCell counts nearby users in one grid cell. Only the cell centre is
// exposed, so callers learn roughly where people are, not who or exactly where.
type NearbyUserCell struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Count     int     `json:"count"`
}

// nearbyUserCellDegrees sizes the grid that blurs nearby users: about 280m
// north–south and roughly 250m east–west at Indian latitudes.
const nearbyUserCellDegrees = 0.0025

func NewLocationService(db *gorm.DB, redis *redis.Client, geoSvc *GeofencingService) *LocationService {
	ls := &LocationService{
		db:                db,
		redis:             redis,
		geofencingService: geoSvc,
		locationChan:      make(chan *models.UserLocation, 1000),
		workerPool:        10,
	}

	// Start worker pool for batch inserts
	for i := 0; i < ls.workerPool; i++ {
		go ls.locationWorker()
	}

	return ls
}

func (ls *LocationService) UpdateUserLocation(userID uint, loc models.UserLocation) error {
	loc.UserID = userID
	loc.RecordedAt = time.Now()

	// Send to buffer channel (non-blocking)
	select {
	case ls.locationChan <- &loc:
	default:
		// Channel full, insert directly
		ls.db.Create(&loc)
	}

	// Update Redis cache
	ls.cacheLocation(userID, loc)

	// Check if in danger zone
	zone, err := ls.geofencingService.CheckDangerZone(loc.Location.Latitude, loc.Location.Longitude)
	if err == nil && zone != nil {
		ls.geofencingService.NotifyDangerZoneEntry(userID, zone.ID)
	}

	return nil
}

func (ls *LocationService) locationWorker() {
	buffer := make([]*models.UserLocation, 0, 100)
	ticker := time.NewTicker(10 * time.Second)

	for {
		select {
		case loc := <-ls.locationChan:
			buffer = append(buffer, loc)

			// Batch insert when buffer full
			if len(buffer) >= 100 {
				ls.batchInsertLocations(buffer)
				buffer = buffer[:0]
			}

		case <-ticker.C:
			// Insert remaining locations periodically
			if len(buffer) > 0 {
				ls.batchInsertLocations(buffer)
				buffer = buffer[:0]
			}
		}
	}
}

func (ls *LocationService) batchInsertLocations(locations []*models.UserLocation) error {
	// GORM CreateInBatches optimizes multi-row inserts natively
	return ls.db.CreateInBatches(locations, 100).Error
}

func (ls *LocationService) GetCurrentLocation(userID uint) (*models.UserLocation, error) {
	// Try Redis cache first (faster and includes recently updated locations)
	key := fmt.Sprintf("location:user_%d", userID)
	ctx := context.Background()

	if ls.redis != nil {
		if exists, _ := ls.redis.Exists(ctx, key).Result(); exists == 1 {
			data, err := ls.redis.HGetAll(ctx, key).Result()
			if err == nil && len(data) > 0 {
				lat, latErr := strconv.ParseFloat(data["lat"], 64)
				lng, lngErr := strconv.ParseFloat(data["lng"], 64)
				ts, tsErr := strconv.ParseInt(data["ts"], 10, 64)

				if latErr == nil && lngErr == nil && tsErr == nil {
					return &models.UserLocation{
						UserID: userID,
						Location: models.Location{
							Latitude:  lat,
							Longitude: lng,
						},
						RecordedAt: time.Unix(ts, 0),
					}, nil
				}
			}
		}
	}

	// Fallback to database (last 5 minutes)
	var location models.UserLocation
	err := ls.db.Where("user_id = ? AND recorded_at > ?", userID, time.Now().Add(-5*time.Minute)).
		Order("recorded_at DESC").
		First(&location).Error

	if err != nil {
		return nil, err
	}

	// Cache for 1 hour actively
	ls.cacheLocation(userID, location)

	return &location, nil
}

func (ls *LocationService) GetNearbyUsers(lat, lng float64, radius int) ([]uint, error) {
	query := `
	SELECT DISTINCT user_id
	FROM user_locations
	WHERE ST_DWithin(
		location::geography,
		ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography,
		?
	)
	AND recorded_at > NOW() - INTERVAL '5 minutes'
	`

	var userIDs []uint
	// Note: GORM uses ? for interpolation normally over $1, $2
	err := ls.db.Raw(query, lng, lat, radius).Scan(&userIDs).Error
	return userIDs, err
}

// GetNearbyUserCells returns how many other users were recently near a point,
// grouped into grid cells. Exact positions and user IDs stay inside the service.
func (ls *LocationService) GetNearbyUserCells(lat, lng float64, radius int, excludeUserID uint) ([]NearbyUserCell, error) {
	users, err := ls.getNearbyUserLocations(lat, lng, radius, excludeUserID)
	if err != nil {
		return nil, err
	}
	return groupNearbyUsersIntoCells(users, nearbyUserCellDegrees), nil
}

// groupNearbyUsersIntoCells counts users per grid cell, keeping the order in
// which each cell is first seen.
func groupNearbyUsersIntoCells(users []NearbyUserLocation, cellDegrees float64) []NearbyUserCell {
	type cellKey struct{ lat, lng int64 }
	indexes := make(map[cellKey]int)
	cells := make([]NearbyUserCell, 0)
	for _, user := range users {
		key := cellKey{
			lat: int64(math.Floor(user.Latitude / cellDegrees)),
			lng: int64(math.Floor(user.Longitude / cellDegrees)),
		}
		if i, ok := indexes[key]; ok {
			cells[i].Count++
			continue
		}
		indexes[key] = len(cells)
		cells = append(cells, NearbyUserCell{
			Latitude:  cellCentre(key.lat, cellDegrees),
			Longitude: cellCentre(key.lng, cellDegrees),
			Count:     1,
		})
	}
	return cells
}

// cellCentre returns the middle of a grid cell, rounded to 6 decimal places.
func cellCentre(index int64, cellDegrees float64) float64 {
	return math.Round((float64(index)+0.5)*cellDegrees*1e6) / 1e6
}

func (ls *LocationService) getNearbyUserLocations(lat, lng float64, radius int, excludeUserID uint) ([]NearbyUserLocation, error) {
	cacheKey := nearbyUsersCacheKey(lat, lng, radius, excludeUserID)
	if users, ok := ls.getCachedNearbyUsers(cacheKey); ok {
		return users, nil
	}

	query := `
	WITH latest_locations AS (
		SELECT DISTINCT ON (user_id)
			user_id,
			ST_Y(location::geometry) AS latitude,
			ST_X(location::geometry) AS longitude,
			recorded_at
		FROM user_locations
		WHERE recorded_at > NOW() - INTERVAL '10 minutes'
		ORDER BY user_id, recorded_at DESC
	)
	SELECT user_id, latitude, longitude, recorded_at
	FROM latest_locations
	WHERE user_id <> ?
	  AND ST_DWithin(
		ST_SetSRID(ST_MakePoint(longitude, latitude), 4326)::geography,
		ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography,
		?
	  )
	ORDER BY ST_Distance(
		ST_SetSRID(ST_MakePoint(longitude, latitude), 4326)::geography,
		ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography
	  ) ASC, recorded_at DESC
	`

	var users []NearbyUserLocation
	err := ls.db.Raw(query, excludeUserID, lng, lat, radius, lng, lat).Scan(&users).Error
	if err == nil {
		ls.cacheNearbyUsers(cacheKey, users)
	}
	return users, err
}

func (ls *LocationService) cacheLocation(userID uint, loc models.UserLocation) {
	if ls.redis == nil {
		return
	}

	key := fmt.Sprintf("location:user_%d", userID)
	data := map[string]interface{}{
		"lat": loc.Location.Latitude,
		"lng": loc.Location.Longitude,
		"ts":  loc.RecordedAt.Unix(),
	}
	ls.redis.HSet(context.Background(), key, data)
	ls.redis.Expire(context.Background(), key, time.Hour)
}

func nearbyUsersCacheKey(lat, lng float64, radius int, excludeUserID uint) string {
	return fmt.Sprintf(
		"location:nearby:%0.4f:%0.4f:%d:%d",
		roundLocationCoord(lat, 4),
		roundLocationCoord(lng, 4),
		radius,
		excludeUserID,
	)
}

func roundLocationCoord(value float64, decimals int) float64 {
	pow := math.Pow10(decimals)
	return math.Round(value*pow) / pow
}

func (ls *LocationService) getCachedNearbyUsers(cacheKey string) ([]NearbyUserLocation, bool) {
	if ls.redis == nil {
		return nil, false
	}

	cached, err := ls.redis.Get(context.Background(), cacheKey).Result()
	if err != nil || cached == "" {
		return nil, false
	}

	var users []NearbyUserLocation
	if err := json.Unmarshal([]byte(cached), &users); err != nil {
		return nil, false
	}

	return users, true
}

func (ls *LocationService) cacheNearbyUsers(cacheKey string, users []NearbyUserLocation) {
	if ls.redis == nil {
		return
	}

	encoded, err := json.Marshal(users)
	if err != nil {
		return
	}

	ls.redis.Set(context.Background(), cacheKey, encoded, 5*time.Second)
}
