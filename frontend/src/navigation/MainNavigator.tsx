import React, { useCallback, useEffect, useRef, useState } from 'react';
import { createBottomTabNavigator } from '@react-navigation/bottom-tabs';
import { MainTabParamList } from '@/types/navigation';
import { MaterialIcons as Icon } from '@expo/vector-icons';
import { EmergencyNavigator } from './EmergencyNavigator';
import { ProfileNavigator } from './ProfileNavigator';
import { colors } from '@/theme/colors';
import { borderRadius, spacing } from '@/theme/spacing';
import { MapNavigator } from './MapNavigator';
import { getFocusedRouteNameFromRoute } from '@react-navigation/native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import * as Notifications from 'expo-notifications';
import { AppState, InteractionManager, Platform } from 'react-native';
import CrimeWebSocketService from '@/services/websocket/CrimeWebSocket';
import { WEBSOCKET_URL } from '@/config/env';
import { useAuthStore } from '@/store/authStore';

import { useShakeDetection } from '@/hooks/useShakeDetection';
import { useAlertStore } from '@/store/alertStore';
import { useLocation } from '@/hooks/useLocation';
import { useSettingsStore } from '@/store/settingsStore';
import { ResponderAlertModal } from '@/screens/emergency/ResponderAlertModal';
import { Alert, Location } from '@/types/models';
import { notificationService, NotificationCategory } from '@/services/notifications/notificationService';
import { notificationApiService } from '@/services/api/notificationService';
import { AppPreparingScreen } from '@/components/common/AppPreparingScreen';

const Tab = createBottomTabNavigator<MainTabParamList>();

export const MainNavigator = () => {
  const insets = useSafeAreaInsets();
  const { isAlertActive, createAlert, activeAlert } = useAlertStore();
  const { currentLocation } = useLocation();
  const { shakeToSOS, priorityAlerts } = useSettingsStore();
  const { isAuthenticated, user } = useAuthStore();
  const [incomingAlert, setIncomingAlert] = useState<Alert | null>(null);
  const [incomingDistance, setIncomingDistance] = useState(0);
  const latestLocationRef = useRef(currentLocation);
  const latestIncomingAlertIdRef = useRef<string | null>(null);
  const appStateRef = useRef(AppState.currentState);
  const pushRegistrationInFlightRef = useRef(false);
  const registeredTokenRef = useRef<string | null>(null);
  const registeredUserIdRef = useRef<string | null>(null);
  const [servicesReady, setServicesReady] = useState(false);

  useEffect(() => {
    if (!isAuthenticated) {
      setServicesReady(false);
      return;
    }

    let isCancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const interactionTask = InteractionManager.runAfterInteractions(() => {
      timer = setTimeout(() => {
        if (!isCancelled) {
          setServicesReady(true);
        }
      }, 650);
    });

    return () => {
      isCancelled = true;
      interactionTask.cancel();
      if (timer) {
        clearTimeout(timer);
      }
    };
  }, [isAuthenticated, user?.id]);
  
  // Global Shake Detection for Emergency SOS
  useShakeDetection({
    enabled: servicesReady && shakeToSOS && !isAlertActive,
    onShake: async () => {
      console.log('Global Shake Detected! Triggering Emergency SOS.');
      if (currentLocation) {
        try {
          await createAlert(currentLocation, true); // Silent mode by default for shake
          console.log('Shake-based Silent Alert Created Successfully.');
        } catch (error) {
          console.error('Failed to create shake-based alert:', error);
        }
      }
    }
  });

  useEffect(() => {
    latestLocationRef.current = currentLocation;
  }, [currentLocation]);

  useEffect(() => {
    latestIncomingAlertIdRef.current = incomingAlert?.id ?? null;
  }, [incomingAlert]);

  const presentIncomingAlert = useCallback((payload: any) => {
    if (!priorityAlerts) {
      return;
    }

    const authUserId = String(useAuthStore.getState().user?.id ?? '');
    const sourceUserId = String(payload?.user?.user_id ?? payload?.user_id ?? '');
    const alertId = String(payload?.alert_id ?? '');

    // The server sends emergency alerts only to the users they were dispatched
    // to, so any alert that arrives here is meant for this user.
    if (!alertId || !authUserId || sourceUserId === authUserId) {
      return;
    }
    if (useAlertStore.getState().activeAlert?.id === alertId || useAlertStore.getState().isAlertActive) {
      return;
    }
    if (latestIncomingAlertIdRef.current === alertId) {
      return;
    }

    const latitude = Number(payload?.location?.latitude ?? payload?.latitude);
    const longitude = Number(payload?.location?.longitude ?? payload?.longitude);
    if (!Number.isFinite(latitude) || !Number.isFinite(longitude)) {
      return;
    }

    const location = { latitude, longitude };
    const createdAt =
      typeof payload?.created_at === 'string'
        ? payload.created_at
        : new Date().toISOString();
    const distanceFromPayload = Number(
      payload?.distance ??
      payload?.distance_m ??
      payload?.distance_meters ??
      0
    );

    setIncomingDistance(
      latestLocationRef.current
        ? Math.round(getDistanceMeters(latestLocationRef.current, location))
        : Math.max(0, Math.round(distanceFromPayload))
    );
    setIncomingAlert({
      id: alertId,
      userId: sourceUserId,
      type: payload?.alert_type === 'check_in' || payload?.alert_type === 'safe_zone'
        ? payload.alert_type
        : 'panic',
      status: 'active',
      location,
      currentRadius: Number(payload?.current_radius || 100),
      usersNotified: Number(payload?.users_notified || 0),
      createdAt,
    });
  }, [priorityAlerts]);

  const syncPushRegistration = useCallback(async () => {
    if (!isAuthenticated || !user?.id || pushRegistrationInFlightRef.current) {
      return;
    }

    pushRegistrationInFlightRef.current = true;
    try {
      const permissionGranted = await notificationService.requestPermission();
      if (!permissionGranted) {
        return;
      }

      const token = await notificationService.getToken();
      if (!token) {
        return;
      }

      const normalizedUserId = String(user.id);
      if (registeredTokenRef.current === token && registeredUserIdRef.current === normalizedUserId) {
        return;
      }

      await notificationApiService.registerToken(
        token,
        Platform.OS === 'ios' ? 'ios' : 'android'
      );
      registeredTokenRef.current = token;
      registeredUserIdRef.current = normalizedUserId;
      console.log('Push token synced for authenticated user.');
    } catch (error) {
      console.warn('Push token sync skipped for now:', error);
    } finally {
      pushRegistrationInFlightRef.current = false;
    }
  }, [isAuthenticated, user?.id]);

  useEffect(() => {
    if (!servicesReady) {
      return;
    }
    void syncPushRegistration();
  }, [servicesReady, syncPushRegistration]);

  useEffect(() => {
    if (!isAuthenticated || !servicesReady) {
      return;
    }

    const subscription = AppState.addEventListener('change', (nextAppState) => {
      appStateRef.current = nextAppState;
      if (nextAppState === 'active') {
        void syncPushRegistration();
      }
    });

    return () => subscription.remove();
  }, [isAuthenticated, servicesReady, syncPushRegistration]);

  useEffect(() => {
    if (!isAuthenticated || !servicesReady || !WEBSOCKET_URL) {
      return;
    }

    CrimeWebSocketService.connect(`${WEBSOCKET_URL}/ws/crime`);

    const handleEmergencyAlert = (data: any) => {
      presentIncomingAlert(data);
    };

    const handleRoomClosed = (data: any) => {
      const closedAlertId = String(data?.alert_id ?? data?.room_id ?? '');
      if (!closedAlertId || !latestIncomingAlertIdRef.current) {
        return;
      }

      const normalizedClosedAlertId = closedAlertId.startsWith('alert_')
        ? closedAlertId.replace(/^alert_/, '')
        : closedAlertId;

      if (normalizedClosedAlertId === latestIncomingAlertIdRef.current) {
        setIncomingAlert(null);
      }
    };

    CrimeWebSocketService.on('emergency_alert', handleEmergencyAlert);
    CrimeWebSocketService.on('room_closed', handleRoomClosed);

    return () => {
      CrimeWebSocketService.off('emergency_alert', handleEmergencyAlert);
      CrimeWebSocketService.off('room_closed', handleRoomClosed);
      CrimeWebSocketService.disconnect();
    };
  }, [isAuthenticated, presentIncomingAlert, servicesReady]);

  useEffect(() => {
    if (!isAuthenticated || !servicesReady) {
      return;
    }

    const handleNotificationPayload = (rawData: Record<string, unknown> | undefined | null) => {
      if (!rawData) {
        return;
      }

      const category = String(rawData.category ?? '');
      if (category === NotificationCategory.EMERGENCY_ALERT) {
        presentIncomingAlert(rawData);
      }
    };

    let isMounted = true;
    const foregroundSubscription = Notifications.addNotificationReceivedListener((notification) => {
      if (appStateRef.current === 'active') {
        handleNotificationPayload(notification.request.content.data as Record<string, unknown>);
      }
    });

    const responseSubscription = Notifications.addNotificationResponseReceivedListener((response) => {
      handleNotificationPayload(response.notification.request.content.data as Record<string, unknown>);
      void Notifications.clearLastNotificationResponseAsync().catch(() => undefined);
    });

    void Notifications.getLastNotificationResponseAsync()
      .then((response) => {
        if (isMounted && response) {
          handleNotificationPayload(response.notification.request.content.data as Record<string, unknown>);
          void Notifications.clearLastNotificationResponseAsync().catch(() => undefined);
        }
      })
      .catch((error) => {
        console.warn('Unable to inspect the last notification response:', error);
      });

    return () => {
      isMounted = false;
      foregroundSubscription.remove();
      responseSubscription.remove();
    };
  }, [isAuthenticated, presentIncomingAlert, servicesReady]);

  useEffect(() => {
    if (activeAlert) {
      setIncomingAlert(null);
    }
  }, [activeAlert]);

  if (!servicesReady) {
    return <AppPreparingScreen />;
  }

  return (
    <>
      <Tab.Navigator
        screenOptions={{
          headerShown: false,
          tabBarActiveTintColor: colors.primary,
          tabBarInactiveTintColor: colors.textSecondary,
          tabBarStyle: {
            height: 52 + insets.bottom,
            paddingTop: 8,
            paddingBottom: insets.bottom > 0 ? insets.bottom : 8,
            backgroundColor: colors.surface,
            borderTopWidth: 1,
            borderTopColor: colors.border,
            position: 'absolute',
            bottom: 0,
            left: 0,
            right: 0,
            elevation: 10,
            overflow: 'visible',
          },
          tabBarLabelStyle: {
            fontSize: 11,
            fontWeight: '700',
            letterSpacing: 0.5,
            marginTop: 4,
          },
        }}
      >
        <Tab.Screen
          name="Map"
          component={MapNavigator}
          options={({ route }) => ({
            tabBarIcon: ({ color, size, focused }) => (
              <Icon name="map" size={size} color={focused ? colors.primary : color} />
            ),
            tabBarLabel: 'MAP',
            tabBarLabelStyle: {
              fontSize: 11,
              fontWeight: '700',
              letterSpacing: 0.5,
              marginTop: 4,
            },
            tabBarStyle: ((route) => {
              const routeName = getFocusedRouteNameFromRoute(route) ?? 'MapDashboard';
              if (['SafeRoute', 'Navigation', 'LocationHistory', 'CrimeDetails'].includes(routeName)) {
                return { display: 'none' };
              }
              return {
                height: 52 + insets.bottom,
                paddingTop: 8,
                paddingBottom: insets.bottom > 0 ? insets.bottom : 8,
                backgroundColor: colors.surface,
                borderTopWidth: 1,
                borderTopColor: colors.border,
                position: 'absolute',
                bottom: 0,
                left: 0,
                right: 0,
                elevation: 10,
                overflow: 'visible',
              };
            })(route),
          })}
        />
        <Tab.Screen
          name="Emergency"
          component={EmergencyNavigator}
          options={({ route }) => ({
            tabBarIcon: ({ color, size, focused }) => (
              <Icon name="notifications" size={size} color={focused ? colors.primary : color} />
            ),
            tabBarLabel: 'ALERTS',
            tabBarLabelStyle: {
              fontSize: 11,
              fontWeight: '700',
              letterSpacing: 0.5,
              marginTop: 4,
            },
            tabBarStyle: ((route) => {
              const routeName = getFocusedRouteNameFromRoute(route) ?? 'EmergencyDashboard';
              if (['EmergencyActive', 'EmergencyResolution', 'ResponderNavigation'].includes(routeName)) {
                return { display: 'none' };
              }
              return {
                height: 52 + insets.bottom,
                paddingTop: 8,
                paddingBottom: insets.bottom > 0 ? insets.bottom : 8,
                backgroundColor: colors.surface,
                borderTopWidth: 1,
                borderTopColor: colors.border,
                position: 'absolute',
                bottom: 0,
                left: 0,
                right: 0,
                elevation: 10,
                overflow: 'visible',
              };
            })(route),
          })}
        />
        <Tab.Screen
          name="Profile"
          component={ProfileNavigator}
          options={{
            tabBarIcon: ({ color, size, focused }) => (
              <Icon name="person" size={size} color={focused ? colors.primary : color} />
            ),
            tabBarLabel: 'PROFILE',
            tabBarLabelStyle: {
              fontSize: 11,
              fontWeight: '700',
              letterSpacing: 0.5,
              marginTop: 4,
            },
          }}
        />
      </Tab.Navigator>
      {incomingAlert ? (
        <ResponderAlertModal
          visible={true}
          alert={incomingAlert}
          distance={incomingDistance}
          onClose={() => setIncomingAlert(null)}
        />
      ) : null}
    </>
  );
};

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
