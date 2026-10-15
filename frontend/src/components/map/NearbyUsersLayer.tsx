import React, { useCallback, useEffect, useRef, useState } from 'react';
import { View, StyleSheet } from 'react-native';
import { Marker } from 'react-native-maps';
import { Location } from '@/types/models';
import { colors } from '@/theme/colors';
import { locationApiService, NearbyUserCell } from '@/services/api/locationApiService';
import Animated, { useSharedValue, useAnimatedStyle, withRepeat, withTiming } from 'react-native-reanimated';

interface NearbyUsersLayerProps {
  userLocation: Location;
  onUsersChange?: (count: number) => void;
}

/**
 * Shows areas with nearby active users as pulsing dots, one per ~250m cell.
 * The server sends only cell centres and counts, never exact positions.
 * Uber/Ola style — no plain Views inside MapView to avoid native crashes.
 */
export const NearbyUsersLayer: React.FC<NearbyUsersLayerProps> = ({ userLocation, onUsersChange }) => {
  const [nearbyCells, setNearbyCells] = useState<NearbyUserCell[]>([]);
  const pulse = useSharedValue(1);
  const lastFetchAtRef = useRef(0);
  const latestLocationRef = useRef(userLocation);
  const lastFetchCenterRef = useRef<Location | null>(null);

  useEffect(() => {
    pulse.value = withRepeat(withTiming(1.5, { duration: 1500 }), -1, true);
  }, []);

  const fetchNearbyUsers = useCallback(async (force = false) => {
    const now = Date.now();
    if (!force && now - lastFetchAtRef.current < 4000) {
      return;
    }
    lastFetchAtRef.current = now;

    // The server already leaves the current user out of these counts.
    const cells = await locationApiService.getNearbyUsers(latestLocationRef.current, 5000, force);

    setNearbyCells(cells);
    onUsersChange?.(cells.reduce((total, cell) => total + cell.count, 0));
    lastFetchCenterRef.current = latestLocationRef.current;
  }, [onUsersChange]);

  useEffect(() => {
    latestLocationRef.current = userLocation;

    const previousFetchCenter = lastFetchCenterRef.current;
    if (!previousFetchCenter || getDistanceMeters(previousFetchCenter, userLocation) >= 150) {
      fetchNearbyUsers(true);
    }
  }, [fetchNearbyUsers, userLocation]);

  useEffect(() => {
    fetchNearbyUsers(true);
    const interval = setInterval(() => fetchNearbyUsers(true), 20000);
    return () => {
      clearInterval(interval);
    };
  }, [fetchNearbyUsers]);

  const animatedPulseStyle = useAnimatedStyle(() => ({
    transform: [{ scale: pulse.value }],
    opacity: 0.6 / pulse.value,
  }));

  return (
    <>
      {nearbyCells.map((cell) => (
        <Marker
          key={`${cell.latitude}:${cell.longitude}`}
          coordinate={{
            latitude: cell.latitude,
            longitude: cell.longitude,
          }}
          anchor={{ x: 0.5, y: 0.5 }}
          tracksViewChanges={false}
        >
          <View style={styles.markerContainer}>
            <Animated.View style={[styles.pulse, animatedPulseStyle]} />
            <View style={styles.userDot} />
          </View>
        </Marker>
      ))}
    </>
  );
};

const styles = StyleSheet.create({
  markerContainer: {
    width: 24,
    height: 24,
    justifyContent: 'center',
    alignItems: 'center',
  },
  pulse: {
    position: 'absolute',
    width: 20,
    height: 20,
    borderRadius: 10,
    backgroundColor: colors.secondary,
  },
  userDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
    backgroundColor: colors.secondary,
    borderWidth: 1.5,
    borderColor: colors.surface,
  },
});

function getDistanceMeters(
  origin: Pick<Location, 'latitude' | 'longitude'>,
  destination: Pick<Location, 'latitude' | 'longitude'>
) {
  const toRadians = (degrees: number) => (degrees * Math.PI) / 180;
  const earthRadius = 6371000;
  const dLat = toRadians(destination.latitude - origin.latitude);
  const dLng = toRadians(destination.longitude - origin.longitude);
  const lat1 = toRadians(origin.latitude);
  const lat2 = toRadians(destination.latitude);

  const a = Math.sin(dLat / 2) ** 2 +
    Math.cos(lat1) * Math.cos(lat2) * Math.sin(dLng / 2) ** 2;

  return 2 * earthRadius * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
}
