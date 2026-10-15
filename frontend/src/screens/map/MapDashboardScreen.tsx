import React, { useState, useRef, useEffect, useCallback } from 'react';
import { View, StyleSheet, Pressable, Platform, Text, Alert as NativeAlert } from 'react-native';
import MapView, { Region, PROVIDER_GOOGLE, UrlTile } from 'react-native-maps';
import { MaterialIcons as Icon } from '@expo/vector-icons';
import CrimeWebSocketService from '@/services/websocket/CrimeWebSocket';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { HeatmapLayer } from '@/components/map/HeatmapLayer';
import { UserLocationMarker } from '@/components/map/UserLocationMarker';
import { DangerZoneMarker } from '@/components/map/DangerZoneMarker';
import { LocationNameOverlay } from '@/components/map/LocationNameOverlay';
import { CurrentLocationButton } from '@/components/map/CurrentLocationButton';
import { QuickStatsCard } from '@/components/map/QuickStatsCard';
import { MapSearchBar } from '@/components/map/MapSearchBar';
import { MapTypeSwitch } from '@/components/map/MapTypeSwitch';
import { NearbyUsersLayer } from '@/components/map/NearbyUsersLayer';
import { DangerZoneAlert } from '@/components/location/DangerZoneAlert';
import { BackgroundLocationIndicator } from '@/components/location/BackgroundLocationIndicator';
import { API_URL, GOOGLE_MAPS_API_KEY } from '@/config/env';
import { useMapStore } from '@/store/mapStore';
import { useAuthStore } from '@/store/authStore';
import { useLocation } from '@/hooks/useLocation';
import { useGeofencing } from '@/hooks/useGeofencing';
import { heatmapService } from '@/services/api/heatmapService';
import { DangerZone } from '@/types/models';
import { colors } from '@/theme/colors';
import { spacing, borderRadius, shadows } from '@/theme/spacing';
import { useNavigation } from '@react-navigation/native';
import CrimeHeatmapOverlay from '@/components/map/CrimeHeatmapOverlay';
import { useAlertStore } from '@/store/alertStore';
import { EmergencyTriggerModal } from '@/screens/emergency/EmergencyTriggerModal';
import Animated, { useSharedValue, useAnimatedStyle, withRepeat, withTiming } from 'react-native-reanimated';

// A crime popup interrupts the user, so it is shown only for incidents close
// by and at most once a minute; the heatmap still refreshes for every update.
const CRIME_ALERT_RADIUS_METERS = 2000;
const CRIME_ALERT_COOLDOWN_MS = 60_000;
// Crime updates arrive in bursts (one per inserted incident), so the heatmap
// reloads once the burst is over rather than once per incident.
const HEATMAP_REFRESH_DEBOUNCE_MS = 2000;

export const MapDashboardScreen = () => {
  const insets = useSafeAreaInsets();
  const navigation = useNavigation();
  const mapRef = useRef<MapView>(null);
  const areaStatsDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastAreaStatsCenterRef = useRef<{ latitude: number; longitude: number } | null>(null);
  const heatmapRefreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastCrimeAlertAtRef = useRef(0);
  const { currentLocation, startTracking, isTracking } = useLocation();
  const latestLocationRef = useRef(currentLocation);
  latestLocationRef.current = currentLocation;
  const { user } = useAuthStore();
  const { mapType, setMapType, currentStats, setCurrentStats } = useMapStore();
  const { isInDangerZone, currentZone } = useGeofencing();
  
  const [region, setRegion] = useState<Region>({
    latitude: currentLocation?.latitude || 28.6139,
    longitude: currentLocation?.longitude || 77.2090,
    latitudeDelta: 0.0922,
    longitudeDelta: 0.0421,
  });
  const [dangerZones, setDangerZones] = useState<DangerZone[]>([]);
  const [showEmergencyModal, setShowEmergencyModal] = useState(false);
  const [showAreaStatsCard, setShowAreaStatsCard] = useState(false);
  const [showMapTypeMenu, setShowMapTypeMenu] = useState(false);
  const [heatmapKey, setHeatmapKey] = useState(0);
  const [showDangerAlert, setShowDangerAlert] = useState(false);
  const [liveNearbyUsers, setLiveNearbyUsers] = useState(0);
  const [selectedPlace, setSelectedPlace] = useState<{ name: string; location: { latitude: number; longitude: number } } | null>(null);
  const [crimeLocationNames, setCrimeLocationNames] = useState<Array<{ name: string; latitude: number; longitude: number; severity?: number }>>([]);
  const [mapBounds, setMapBounds] = useState({
    north: 0,
    south: 0,
    east: 0,
    west: 0,
  });

  const { createAlert } = useAlertStore();

  const openEmergencyActiveScreen = useCallback(() => {
    (navigation.getParent() as any)?.navigate('Emergency', { screen: 'EmergencyActive' });
  }, [navigation]);

  // Pulse animation for the SOS button
  const pulseScale = useSharedValue(1);
  const pulseOpacity = useSharedValue(0.3);

  useEffect(() => {
    pulseScale.value = withRepeat(withTiming(1.6, { duration: 1500 }), -1, false);
    pulseOpacity.value = withRepeat(withTiming(0, { duration: 1500 }), -1, false);
  }, []);

  const animatedPulseStyle = useAnimatedStyle(() => ({
    transform: [{ scale: pulseScale.value }],
    opacity: pulseOpacity.value,
  }));

  useEffect(() => {
    void startTracking().catch((error) => {
      console.warn('Map location tracking will retry after app startup settles:', error);
    });
    const initLat = currentLocation?.latitude || 28.6139;
    const initLng = currentLocation?.longitude || 77.2090;
    scheduleAreaStatsFetch(initLat, initLng, 0);
  }, []);

  useEffect(() => {
    if (isInDangerZone && currentZone) {
      setShowDangerAlert(true);
    }
  }, [isInDangerZone, currentZone]);

  useEffect(() => {
    if (currentLocation) {
      const newRegion = {
        latitude: currentLocation.latitude,
        longitude: currentLocation.longitude,
        latitudeDelta: 0.0922,
        longitudeDelta: 0.0421,
      };
      setRegion(newRegion);
      mapRef.current?.animateToRegion(newRegion, 1000);
      scheduleAreaStatsFetch(currentLocation.latitude, currentLocation.longitude, 0);
    }
  }, [currentLocation]);

  useEffect(() => {
    const scheduleHeatmapRefresh = () => {
      if (heatmapRefreshTimerRef.current) {
        clearTimeout(heatmapRefreshTimerRef.current);
      }
      heatmapRefreshTimerRef.current = setTimeout(() => {
        heatmapRefreshTimerRef.current = null;
        setHeatmapKey(prev => prev + 1);
      }, HEATMAP_REFRESH_DEBOUNCE_MS);
    };

    const handleNewCrime = (data: any) => {
      scheduleHeatmapRefresh();

      const location = latestLocationRef.current;
      const latitude = Number(data?.lat);
      const longitude = Number(data?.lng);
      if (!location || !Number.isFinite(latitude) || !Number.isFinite(longitude)) {
        return;
      }
      if (getDistanceMeters(location, { latitude, longitude }) > CRIME_ALERT_RADIUS_METERS) {
        return;
      }
      const now = Date.now();
      if (now - lastCrimeAlertAtRef.current < CRIME_ALERT_COOLDOWN_MS) {
        return;
      }
      lastCrimeAlertAtRef.current = now;

      NativeAlert.alert(
        '🚨 Crime Alert',
        `${data.crime_type} reported nearby`,
        [
          { text: 'View', onPress: () => {
            mapRef.current?.animateToRegion({
              latitude: data.lat,
              longitude: data.lng,
              latitudeDelta: 0.01,
              longitudeDelta: 0.01,
            });
          }},
          { text: 'Dismiss' }
        ]
      );
    };

    const handleResponderAccepted = (data: any) => {
      const authUserId = String(useAuthStore.getState().user?.id ?? '');
      const liveAlert = useAlertStore.getState().activeAlert;
      const targetUserId = String(data.target_user_id ?? '');

      if (!liveAlert?.id || String(data.alert_id ?? '') !== String(liveAlert.id)) {
        return;
      }
      if (targetUserId && targetUserId !== authUserId) {
        return;
      }

      NativeAlert.alert(
        '✅ Help is on the way!',
        `A volunteer is ${Math.round(data.distance)}m away and will arrive in approx ${data.eta} min.`
      );
    };

    CrimeWebSocketService.on('crime_added', handleNewCrime);
    CrimeWebSocketService.on('crime_batch', scheduleHeatmapRefresh);
    CrimeWebSocketService.on('responder_accepted', handleResponderAccepted);

    return () => {
      CrimeWebSocketService.off('crime_added', handleNewCrime);
      CrimeWebSocketService.off('crime_batch', scheduleHeatmapRefresh);
      CrimeWebSocketService.off('responder_accepted', handleResponderAccepted);
      if (heatmapRefreshTimerRef.current) {
        clearTimeout(heatmapRefreshTimerRef.current);
      }
    };
  }, []);

  const fetchAreaStats = useCallback(async (lat: number, lng: number) => {
    try {
      const stats = await heatmapService.getStatistics(lat, lng);
      setCurrentStats(stats);
    } catch (error) {
      console.warn('Area stats temporarily unavailable; keeping the current map stats.', error);
    }
  }, [setCurrentStats]);

  const scheduleAreaStatsFetch = useCallback((lat: number, lng: number, delayMs = 600) => {
    const nextCenter = { latitude: lat, longitude: lng };
    if (
      lastAreaStatsCenterRef.current &&
      getDistanceMeters(lastAreaStatsCenterRef.current, nextCenter) < 120
    ) {
      return;
    }

    if (areaStatsDebounceRef.current) {
      clearTimeout(areaStatsDebounceRef.current);
    }

    areaStatsDebounceRef.current = setTimeout(() => {
      lastAreaStatsCenterRef.current = nextCenter;
      fetchAreaStats(lat, lng);
    }, delayMs);
  }, [fetchAreaStats]);

  useEffect(() => {
    return () => {
      if (areaStatsDebounceRef.current) {
        clearTimeout(areaStatsDebounceRef.current);
      }
    };
  }, []);

  const handleNearbyUsersChange = useCallback((count: number) => {
    setLiveNearbyUsers(count);
  }, []);

  const handleRegionChange = (newRegion: Region) => {
    setRegion(newRegion);
    const bounds = {
      north: newRegion.latitude + newRegion.latitudeDelta / 2,
      south: newRegion.latitude - newRegion.latitudeDelta / 2,
      east: newRegion.longitude + newRegion.longitudeDelta / 2,
      west: newRegion.longitude - newRegion.longitudeDelta / 2,
    };
    setMapBounds(bounds);
    scheduleAreaStatsFetch(newRegion.latitude, newRegion.longitude);
  };

  const handleCenterLocation = () => {
    if (currentLocation) {
      mapRef.current?.animateToRegion({
        latitude: currentLocation.latitude,
        longitude: currentLocation.longitude,
        latitudeDelta: 0.01,
        longitudeDelta: 0.01,
      }, 1000);
    }
  };

  const handleEmergencyTrigger = useCallback(async () => {
    if (!currentLocation) {
      NativeAlert.alert('Error', 'Cannot get your current location.');
      return;
    }

    try {
      await createAlert({
        latitude: currentLocation.latitude,
        longitude: currentLocation.longitude,
      }, false);
      openEmergencyActiveScreen();
    } catch (error) {
      console.warn('Error creating alert:', error);
      NativeAlert.alert('Error', 'Failed to trigger SOS alert.');
    }
  }, [currentLocation, createAlert, openEmergencyActiveScreen]);

  const handleDangerZonePress = (zone: DangerZone) => {
    console.log('Danger zone pressed:', zone);
  };

  return (
    <View style={styles.container}>
      <MapView
        ref={mapRef}
        style={styles.map}
        provider={Platform.OS === 'android' ? PROVIDER_GOOGLE : undefined}
        mapType={mapType}
        initialRegion={region}
        onRegionChangeComplete={handleRegionChange}
        showsUserLocation={true}
        showsMyLocationButton={false}
        showsCompass={false}
        showsScale={false}
        onPress={() => {
          setShowMapTypeMenu(false);
          setSelectedPlace(null);
        }}
      >
        <CrimeHeatmapOverlay
          key={`heatmap-${heatmapKey}`}
          mapRef={mapRef as React.RefObject<MapView>}
          bounds={mapBounds}
          onLocationNamesLoaded={setCrimeLocationNames}
        />
        
        {/* Location name markers removed - heatmap provides visual indication */}
        
        {currentLocation && (
          <>
            <UserLocationMarker location={currentLocation} />
            <NearbyUsersLayer userLocation={currentLocation} onUsersChange={handleNearbyUsersChange} />
          </>
        )}

        {dangerZones.map((zone) => (
          <DangerZoneMarker
            key={zone.id}
            zone={zone}
            onPress={handleDangerZonePress}
          />
        ))}
      </MapView>

      <View style={[styles.profileBar, { top: insets.top + spacing.sm }]}>
        <Pressable 
          style={styles.profileButton}
          onPress={() => navigation.navigate('Profile' as never)}
        >
          <View style={styles.avatarPlaceholder}>
            <Icon name="verified-user" size={20} color={colors.secondary} />
          </View>
          <View style={styles.profileTextContainer}>
            <Text style={styles.profileTitle} numberOfLines={1}>{user?.name?.split(' ')[0] || 'Safety'}</Text>
            <View style={styles.statusRow}>
              <View style={styles.statusDot} />
              <Text style={styles.profileStatus}>LIVE PROTECTION ACTIVE</Text>
            </View>
          </View>
        </Pressable>

        <View style={styles.topRightActions}>
          <BackgroundLocationIndicator
            isActive={isTracking}
            batteryImpact="low"
            onPress={() => navigation.navigate('LocationHistory' as never)}
          />
        </View>
      </View>

      <MapSearchBar
        topOffset={insets.top + (Platform.OS === 'ios' ? 85 : 75)}
        currentLocation={currentLocation || undefined}
        onSelectLocation={(location) => {
          setSelectedPlace(location);
          mapRef.current?.animateToRegion(
            {
              ...location.location,
              latitudeDelta: 0.01,
              longitudeDelta: 0.01,
            },
            1000
          );
        }}
        style={showEmergencyModal ? { display: 'none' } : undefined}
      />

      {currentLocation && (
        <Pressable
          style={[styles.nearbyBadge, { top: insets.top + 185 }]}
          onPress={() => setShowAreaStatsCard(true)}
        >
          <View style={styles.onlineDot} />
          <Text style={styles.nearbyText}>{liveNearbyUsers} Active Citizens Nearby</Text>
        </Pressable>
      )}

      {selectedPlace && (
        <View style={[styles.locationActions, { bottom: insets.bottom + 110 }]}>
          <View style={styles.locationInfo}>
            <Text style={styles.locationName} numberOfLines={1}>{selectedPlace.name}</Text>
          </View>
          <Pressable 
            style={styles.directionsButton}
            onPress={() => {
              const coords = `${selectedPlace.location.latitude},${selectedPlace.location.longitude}`;
              (navigation as any).navigate('SafeRoute', { destination: coords });
            }}
          >
            <Icon name="directions" size={20} color={colors.surface} />
            <Text style={styles.directionsButtonText}>Safe Path</Text>
          </Pressable>
          <Pressable style={styles.closeActionsButton} onPress={() => setSelectedPlace(null)}>
            <Icon name="close" size={20} color={colors.textSecondary} />
          </Pressable>
        </View>
      )}

      <View style={[styles.layersButtonContainer, { top: insets.top + 135 }]}>
        <Pressable 
          style={[styles.layersFAB, showMapTypeMenu && styles.layersFABActive]} 
          onPress={() => setShowMapTypeMenu(!showMapTypeMenu)}
        >
          <Icon name="layers" size={24} color={showMapTypeMenu ? colors.surface : colors.textPrimary} />
        </Pressable>
        
        {showMapTypeMenu && (
          <View style={styles.mapTypeMenuContainer}>
            <MapTypeSwitch 
              currentType={mapType} 
              onTypeChange={(type) => {
                setMapType(type);
                setShowMapTypeMenu(false);
              }} 
            />
          </View>
        )}
      </View>

      <CurrentLocationButton 
        onPress={handleCenterLocation} 
        style={{ bottom: insets.bottom + 110 }} 
      />
      
      <View style={[styles.sosContainer, { bottom: insets.bottom + (Platform.OS === 'ios' ? 88 : 72) }]}>
        <Animated.View style={[styles.sosPulse, animatedPulseStyle]} />
        <Pressable 
          style={({ pressed }) => [
            styles.sosButtonInner,
            pressed && styles.sosButtonPressed
          ]}
          onPress={() => setShowEmergencyModal(true)}
          onLongPress={handleEmergencyTrigger}
          delayLongPress={1000}
        >
          <Icon name="wifi-tethering" size={32} color={colors.surface} />
          <Text style={styles.sosText}>SOS</Text>
        </Pressable>
      </View>

      <EmergencyTriggerModal
        visible={showEmergencyModal}
        onClose={() => setShowEmergencyModal(false)}
      />

      <QuickStatsCard
        visible={showAreaStatsCard}
        onClose={() => setShowAreaStatsCard(false)}
        stats={currentStats}
        onViewCrimeHistory={() => {
          setShowAreaStatsCard(false);
          navigation.navigate('CrimeDetails' as never);
        }}
        onPlanSafeRoute={() => {
          setShowAreaStatsCard(false);
          navigation.navigate('SafeRoute' as never);
        }}
        onReportIncident={() => {
          setShowAreaStatsCard(false);
          NativeAlert.alert('Report Incident', 'Incident reporting is available from the crime details screen.');
          navigation.navigate('CrimeDetails' as never);
        }}
      />

      {currentZone && (
        <DangerZoneAlert
          visible={showDangerAlert}
          zone={currentZone}
          onDismiss={() => setShowDangerAlert(false)}
          onAlertContacts={() => {
            setShowDangerAlert(false);
          }}
          onEnableSafeRoute={() => {
            setShowDangerAlert(false);
            navigation.navigate('SafeRoute' as never);
          }}
        />
      )}
    </View>
  );
};

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.background,
  },
  map: {
    ...StyleSheet.absoluteFillObject,
  },
  profileBar: {
    position: 'absolute',
    left: spacing.lg,
    right: spacing.lg,
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    zIndex: 10,
  },
  profileButton: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: colors.surfaceTranslucent,
    paddingVertical: spacing.sm,
    paddingHorizontal: spacing.md,
    borderRadius: borderRadius.lg,
    ...shadows.medium,
    borderWidth: 1,
    borderColor: 'rgba(255, 255, 255, 0.3)',
  },
  avatarPlaceholder: {
    width: 36,
    height: 36,
    borderRadius: 18,
    backgroundColor: 'rgba(16, 185, 129, 0.1)',
    justifyContent: 'center',
    alignItems: 'center',
    marginRight: spacing.sm,
  },
  profileTextContainer: {
    justifyContent: 'center',
  },
  profileTitle: {
    fontSize: 14,
    fontWeight: '700',
    color: colors.textPrimary,
  },
  profileStatus: {
    fontSize: 9,
    color: colors.secondary,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  statusRow: {
    flexDirection: 'row',
    alignItems: 'center',
    marginTop: 1,
  },
  statusDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: colors.secondary,
    marginRight: 4,
  },
  topRightActions: {
    flexDirection: 'row',
    alignItems: 'center',
  },
  locationActions: {
    position: 'absolute',
    left: spacing.lg,
    right: spacing.lg,
    backgroundColor: colors.surfaceTranslucent,
    borderRadius: borderRadius.xl,
    padding: spacing.md,
    flexDirection: 'row',
    alignItems: 'center',
    ...shadows.premium,
    zIndex: 15,
    borderWidth: 1,
    borderColor: 'rgba(255, 255, 255, 0.4)',
  },
  locationInfo: {
    flex: 1,
    marginRight: spacing.md,
  },
  locationName: {
    fontSize: 16,
    fontWeight: '600',
    color: colors.textPrimary,
  },
  directionsButton: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: colors.primary,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
    borderRadius: borderRadius.md,
    marginRight: spacing.sm,
    ...shadows.small,
  },
  directionsButtonText: {
    color: colors.surface,
    fontWeight: 'bold',
    marginLeft: spacing.xs,
  },
  closeActionsButton: {
    padding: spacing.sm,
    backgroundColor: 'rgba(0,0,0,0.05)',
    borderRadius: 20,
  },
  layersButtonContainer: {
    position: 'absolute',
    right: spacing.lg,
    zIndex: 5,
  },
  layersFAB: {
    width: 44,
    height: 44,
    borderRadius: 22,
    backgroundColor: colors.surfaceTranslucent,
    justifyContent: 'center',
    alignItems: 'center',
    ...shadows.medium,
    borderWidth: 1,
    borderColor: 'rgba(255, 255, 255, 0.3)',
  },
  layersFABActive: {
    backgroundColor: colors.primary,
    borderColor: colors.primary,
  },
  mapTypeMenuContainer: {
    position: 'absolute',
    top: 52,
    right: 0,
    width: 140,
    zIndex: 10,
  },
  sosContainer: {
    position: 'absolute',
    alignSelf: 'center',
    zIndex: 20,
    alignItems: 'center',
    justifyContent: 'center',
  },
  sosPulse: {
    position: 'absolute',
    width: 90,
    height: 90,
    borderRadius: 45,
    backgroundColor: '#FF3B30',
  },
  sosButtonInner: {
    width: 80,
    height: 80,
    borderRadius: 40,
    backgroundColor: '#FF3B30',
    justifyContent: 'center',
    alignItems: 'center',
    ...shadows.premium,
    shadowColor: '#FF3B30',
    shadowOpacity: 0.5,
    shadowRadius: 15,
    elevation: 10,
    borderWidth: 4,
    borderColor: 'rgba(255, 255, 255, 0.4)',
  },
  sosButtonPressed: {
    transform: [{ scale: 0.9 }],
    backgroundColor: '#D10000',
  },
  sosText: {
    color: colors.surface,
    fontSize: 14,
    fontWeight: '900',
    marginTop: 2,
    letterSpacing: 1,
  },
  nearbyBadge: {
    position: 'absolute',
    left: spacing.lg,
    backgroundColor: 'rgba(255, 255, 255, 0.95)',
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: borderRadius.pill,
    flexDirection: 'row',
    alignItems: 'center',
    borderWidth: 1,
    borderColor: 'rgba(0, 0, 0, 0.05)',
    ...shadows.small,
    zIndex: 10,
  },
  onlineDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: colors.success,
    marginRight: 6,
  },
  nearbyText: {
    fontSize: 10,
    color: colors.textPrimary,
    fontWeight: '700',
    letterSpacing: 0.3,
    textTransform: 'uppercase',
  },
  modalOverlay: {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    zIndex: 9999,
  },
});

function getDistanceMeters(
  origin: { latitude: number; longitude: number },
  destination: { latitude: number; longitude: number }
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
