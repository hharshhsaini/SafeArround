import Constants from 'expo-constants';
import { Platform } from 'react-native';

const RAILWAY_API_URL = 'https://safearound-backend-production.up.railway.app/api/v1';
const RAILWAY_WS_URL = 'wss://safearound-backend-production.up.railway.app';

const getDevHost = () => {
  const debuggerHost = Constants.expoConfig?.hostUri || Constants.manifest?.debuggerHost || Constants.manifest2?.extra?.expoGo?.debuggerHost;
  if (debuggerHost) {
    return debuggerHost.split(':')[0];
  }
  if (Platform.OS === 'android') {
    return '10.0.2.2';
  }
  return 'localhost';
};

const normalizeOptionalConfig = (value?: string) => {
  if (!value || /^your[-_]/i.test(value)) {
    return '';
  }
  return value;
};

// Use local backend in development, Railway in production
export const API_URL = process.env.EXPO_PUBLIC_API_URL || RAILWAY_API_URL;
export const WEBSOCKET_URL = process.env.EXPO_PUBLIC_WS_URL || RAILWAY_WS_URL;
export const GOOGLE_MAPS_API_KEY = normalizeOptionalConfig(process.env.EXPO_PUBLIC_GOOGLE_MAPS_API_KEY);

// Development WebSocket URL candidates for auto-discovery
export const DEV_WS_URL_CANDIDATES = __DEV__ ? [
  `ws://10.19.11.176:8001/ws/crime`,
  `ws://${getDevHost()}:8001/ws/crime`,
  `ws://localhost:8001/ws/crime`,
  `ws://10.0.2.2:8001/ws/crime`,
] : [];
