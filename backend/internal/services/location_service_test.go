package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupNearbyUsersIntoCellsHidesExactPositions(t *testing.T) {
	users := []NearbyUserLocation{
		{UserID: 1, Latitude: 28.61301, Longitude: 77.20902},
		{UserID: 2, Latitude: 28.61399, Longitude: 77.20988}, // same cell as user 1
		{UserID: 3, Latitude: 28.62010, Longitude: 77.21510}, // a neighbouring area
	}

	cells := groupNearbyUsersIntoCells(users, nearbyUserCellDegrees)

	require.Len(t, cells, 2)
	assert.Equal(t, NearbyUserCell{Latitude: 28.61375, Longitude: 77.20875, Count: 2}, cells[0])
	assert.Equal(t, NearbyUserCell{Latitude: 28.62125, Longitude: 77.21625, Count: 1}, cells[1])
}

func TestGroupNearbyUsersIntoCellsWithNoUsers(t *testing.T) {
	cells := groupNearbyUsersIntoCells(nil, nearbyUserCellDegrees)

	require.NotNil(t, cells)
	assert.Empty(t, cells)
}
