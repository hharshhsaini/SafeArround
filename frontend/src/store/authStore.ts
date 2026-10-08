import { create } from 'zustand';
import AsyncStorage from '@react-native-async-storage/async-storage';
import { BackendUser, LoginRequest, SetupProfileRequest } from '@/types/api';
import { authService } from '@/services/api/authService';

// AsyncStorage keys
const KEYS = {
  user: 'auth:user',
  accessToken: 'auth:accessToken',
  refreshToken: 'auth:refreshToken',
  version: 'auth:version',
};

// Bump this number whenever you want to force-clear old sessions
// (e.g. after major auth changes, schema updates, etc.)
const AUTH_VERSION = '4';


interface AuthState {
  user: BackendUser | null;
  accessToken: string | null;
  refreshToken: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  error: string | null;

  setUser: (user: BackendUser | null) => void;
  setTokens: (accessToken: string, refreshToken: string) => void;

  sendOTP: (phone: string, type?: 'signup' | 'login') => Promise<void>;
  verifyOTP: (phone: string, otp: string) => Promise<void>;
  setupProfile: (data: SetupProfileRequest) => Promise<void>;
  updateProfile: (data: Partial<SetupProfileRequest>) => Promise<void>;
  logIn: (data: LoginRequest) => Promise<void>;
  logOut: () => Promise<void>;
  refreshAccessToken: () => Promise<void>;
  loadPersistedAuth: () => Promise<void>;
  clearError: () => void;
  setError: (error: string | null) => void;
}

export const useAuthStore = create<AuthState>((set, get) => ({
  user: null,
  accessToken: null,
  refreshToken: null,
  isAuthenticated: false,
  isLoading: true,
  error: null,

  setUser: (user) => {
    set({ user, isAuthenticated: !!user });
    if (user) {
      AsyncStorage.setItem(KEYS.user, JSON.stringify(user));
    } else {
      AsyncStorage.removeItem(KEYS.user);
    }
  },

  setTokens: (accessToken, refreshToken) => {
    set({ accessToken, refreshToken });
    AsyncStorage.setItem(KEYS.accessToken, accessToken);
    AsyncStorage.setItem(KEYS.refreshToken, refreshToken);
  },

  sendOTP: async (phone, type) => {
    try {
      set({ error: null });
      await authService.sendOTP(phone, type);
    } catch (error: any) {
      const msg = error.response?.data?.error || 'Failed to send OTP';
      set({ error: msg });
      throw new Error(msg);
    }
  },

  verifyOTP: async (phone, otp) => {
    try {
      set({ error: null });
      const response = await authService.verifyOTP(phone, otp);
      get().setTokens(response.tokens.access, response.tokens.refresh);
      get().setUser(response.user);
    } catch (error: any) {
      const msg = error.response?.data?.error || 'OTP verification failed';
      set({ error: msg });
      throw new Error(msg);
    }
  },

  setupProfile: async (data) => {
    try {
      set({ error: null });
      const token = get().accessToken;
      if (!token) throw new Error('Not authenticated');
      const result = await authService.setupProfile(data, token);
      get().setUser(result.user);
    } catch (error: any) {
      const msg = error.response?.data?.error || 'Profile setup failed';
      set({ error: msg });
      throw new Error(msg);
    }
  },

  updateProfile: async (data) => {
    try {
      set({ error: null });
      const token = get().accessToken;
      if (!token) throw new Error('Not authenticated');
      const result = await authService.updateProfile(data, token);
      get().setUser(result.user);
    } catch (error: any) {
      const msg = error.response?.data?.error || 'Profile update failed';
      set({ error: msg });
      throw new Error(msg);
    }
  },

  logIn: async (data) => {
    try {
      set({ error: null });
      const response = await authService.login(data);
      get().setTokens(response.tokens.access, response.tokens.refresh);
      get().setUser(response.user);
    } catch (error: any) {
      const msg = error.response?.data?.error || 'Login failed';
      set({ error: msg });
      throw new Error(msg);
    }
  },

  logOut: async () => {
    try {
      await authService.logout();
    } catch (error: any) {
      if (error?.response?.status !== 401) {
        console.warn('Logout error:', error);
      }
    } finally {
      set({ user: null, accessToken: null, refreshToken: null, isAuthenticated: false });
      await AsyncStorage.multiRemove([KEYS.user, KEYS.accessToken, KEYS.refreshToken]);
    }
  },

  refreshAccessToken: async () => {
    try {
      const currentRefreshToken = get().refreshToken;
      if (!currentRefreshToken) throw new Error('No refresh token');
      const tokens = await authService.refreshToken(currentRefreshToken);
      get().setTokens(tokens.access, tokens.refresh);
    } catch (err: any) {
      // Silently handle 401 errors (expired/invalid tokens) during app startup
      const is401 = err?.response?.status === 401;
      if (!is401 && get().isAuthenticated) {
        console.error('Token refresh failed:', err);
      }
      // Clear auth state silently for expired tokens
      set({ user: null, accessToken: null, refreshToken: null, isAuthenticated: false });
      await AsyncStorage.multiRemove([KEYS.user, KEYS.accessToken, KEYS.refreshToken]);
      throw err;
    }
  },

  loadPersistedAuth: async () => {
    try {
      // ── STEP 0: Version-based flush ──────────────────────────────────────────
      // If AUTH_VERSION changed (e.g. dev token flush, schema change), wipe everything
      const storedVersion = await AsyncStorage.getItem(KEYS.version);
      if (storedVersion !== AUTH_VERSION) {
        await AsyncStorage.multiRemove([KEYS.user, KEYS.accessToken, KEYS.refreshToken]);
        await AsyncStorage.setItem(KEYS.version, AUTH_VERSION);
        console.log('Auth version changed — cleared stale session, redirecting to login');
        set({ user: null, accessToken: null, refreshToken: null, isAuthenticated: false, isLoading: false });
        return;
      }
      // ─────────────────────────────────────────────────────────────────────────

      const [userStr, accessToken, refreshToken] = await AsyncStorage.multiGet([
        KEYS.user,
        KEYS.accessToken,
        KEYS.refreshToken,
      ]);

      const user = userStr[1] ? JSON.parse(userStr[1]) : null;
      const access = accessToken[1];
      const refresh = refreshToken[1];

      // Nothing stored → go to login
      if (!user || !access || !refresh) {
        set({ isLoading: false });
        return;
      }

      // Validate the stored token with the backend
      try {
        console.log('--- DATABASE AUTH CHECK ---');
        console.log('Validating user session with backend database...');
        
        const { apiClient } = require('@/services/api/client');
        await apiClient.get('/users/profile', {
          headers: { Authorization: `Bearer ${access}` },
          timeout: 5000,
        });

        // Token is valid → restore auth
        console.log('Database check SUCCESS: Session is valid.');
        set({ user, accessToken: access, refreshToken: refresh, isAuthenticated: true });
      } catch (validationError: any) {
        const isNetworkError = !validationError?.response;

        if (isNetworkError) {
          // Backend unreachable (offline) → trust cached data
          console.warn('Backend offline — using cached auth');
          set({ user, accessToken: access, refreshToken: refresh, isAuthenticated: true });
        } else {
          // Token is invalid/expired (401, 403) → try refresh
          try {
            const { apiClient } = require('@/services/api/client');
            const refreshResponse = await apiClient.post('/auth/refresh', {
              refresh_token: refresh,
            });
            const newTokens = refreshResponse.data?.tokens;
            if (newTokens?.access && newTokens?.refresh) {
              await AsyncStorage.setItem(KEYS.accessToken, newTokens.access);
              await AsyncStorage.setItem(KEYS.refreshToken, newTokens.refresh);
              set({ user, accessToken: newTokens.access, refreshToken: newTokens.refresh, isAuthenticated: true });
            } else {
              throw new Error('Invalid refresh response');
            }
          } catch (e) {
            // Refresh also failed → clear everything and show login
            console.warn('Session expired — redirecting to login');
            await AsyncStorage.multiRemove([KEYS.user, KEYS.accessToken, KEYS.refreshToken]);
            set({ user: null, accessToken: null, refreshToken: null, isAuthenticated: false });
          }
        }
      }
    } catch (error) {
      console.error('Failed to load persisted auth:', error);
      // On any unexpected error, clear auth for safety
      await AsyncStorage.multiRemove([KEYS.user, KEYS.accessToken, KEYS.refreshToken]);
    } finally {
      set({ isLoading: false });
    }
  },

  clearError: () => set({ error: null }),
  setError: (error) => set({ error }),
}));
