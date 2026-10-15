import { create } from 'zustand';
import AsyncStorage from '@react-native-async-storage/async-storage';
import * as SecureStore from 'expo-secure-store';
import { BackendUser, LoginRequest, SetupProfileRequest } from '@/types/api';
import { authService } from '@/services/api/authService';

// AsyncStorage keys
const KEYS = {
  user: 'auth:user',
  version: 'auth:version',
};

// Tokens live in the OS keychain/keystore. SecureStore keys may only contain
// letters, digits, ".", "-" and "_".
const SECURE_KEYS = {
  accessToken: 'auth.accessToken',
  refreshToken: 'auth.refreshToken',
};

// Where older app versions kept tokens, in plain AsyncStorage.
const LEGACY_TOKEN_KEYS = ['auth:accessToken', 'auth:refreshToken'];

// Readable after the first unlock since boot, so background location and SOS
// work can still authenticate while the phone is locked.
const SECURE_STORE_OPTIONS: SecureStore.SecureStoreOptions = {
  keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK,
};

const saveTokens = async (accessToken: string, refreshToken: string) => {
  await Promise.all([
    SecureStore.setItemAsync(SECURE_KEYS.accessToken, accessToken, SECURE_STORE_OPTIONS),
    SecureStore.setItemAsync(SECURE_KEYS.refreshToken, refreshToken, SECURE_STORE_OPTIONS),
  ]);
};

// Reads the stored tokens, moving any that an older app version left in
// AsyncStorage into SecureStore.
const loadTokens = async (): Promise<{ access: string | null; refresh: string | null }> => {
  const [access, refresh] = await Promise.all([
    SecureStore.getItemAsync(SECURE_KEYS.accessToken, SECURE_STORE_OPTIONS),
    SecureStore.getItemAsync(SECURE_KEYS.refreshToken, SECURE_STORE_OPTIONS),
  ]);
  if (access && refresh) {
    return { access, refresh };
  }

  const [[, legacyAccess], [, legacyRefresh]] = await AsyncStorage.multiGet(LEGACY_TOKEN_KEYS);
  if (legacyAccess && legacyRefresh) {
    await saveTokens(legacyAccess, legacyRefresh);
    await AsyncStorage.multiRemove(LEGACY_TOKEN_KEYS);
    return { access: legacyAccess, refresh: legacyRefresh };
  }

  return { access: null, refresh: null };
};

const clearPersistedAuth = async () => {
  await Promise.all([
    AsyncStorage.multiRemove([KEYS.user, ...LEGACY_TOKEN_KEYS]),
    SecureStore.deleteItemAsync(SECURE_KEYS.accessToken, SECURE_STORE_OPTIONS),
    SecureStore.deleteItemAsync(SECURE_KEYS.refreshToken, SECURE_STORE_OPTIONS),
  ]);
};

// Bump this number whenever you want to force-clear old sessions
// (e.g. after major auth changes, schema updates, etc.)
const AUTH_VERSION = '4';

// The server rotates refresh tokens, so a second refresh running at the same
// time would present a token that was just revoked and sign the user out.
// Concurrent callers share this in-flight refresh instead.
let refreshInFlight: Promise<void> | null = null;


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
  resetPassword: (phone: string, otp: string, newPassword: string) => Promise<void>;
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
    saveTokens(accessToken, refreshToken).catch((error) => {
      console.warn('Failed to persist auth tokens:', error);
    });
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

  resetPassword: async (phone, otp, newPassword) => {
    try {
      set({ error: null });
      await authService.resetPassword(phone, otp, newPassword);
    } catch (error: any) {
      set({ error: error.response?.data?.error || 'Password reset failed' });
      // Rethrown as-is so the screen can tell a wrong code (401) apart.
      throw error;
    }
  },

  logOut: async () => {
    try {
      // Sending the refresh token lets the server revoke this session.
      await authService.logout(get().refreshToken);
    } catch (error: any) {
      if (error?.response?.status !== 401) {
        console.warn('Logout error:', error);
      }
    } finally {
      set({ user: null, accessToken: null, refreshToken: null, isAuthenticated: false });
      await clearPersistedAuth();
    }
  },

  refreshAccessToken: () => {
    if (refreshInFlight) {
      return refreshInFlight;
    }

    refreshInFlight = (async () => {
      const currentRefreshToken = get().refreshToken;
      try {
        if (!currentRefreshToken) throw new Error('No refresh token');
        const tokens = await authService.refreshToken(currentRefreshToken);
        get().setTokens(tokens.access, tokens.refresh);
      } catch (err: any) {
        const is401 = err?.response?.status === 401;
        if (!is401 && get().isAuthenticated) {
          console.warn('Token refresh failed:', err);
        }
        // Only sign out when the server rejected the session. A network or
        // server error says nothing about it, so keep it for a later retry.
        if (!currentRefreshToken || is401) {
          set({ user: null, accessToken: null, refreshToken: null, isAuthenticated: false });
          await clearPersistedAuth();
        }
        throw err;
      }
    })().finally(() => {
      refreshInFlight = null;
    });

    return refreshInFlight;
  },

  loadPersistedAuth: async () => {
    try {
      // ── STEP 0: Version-based flush ──────────────────────────────────────────
      // If AUTH_VERSION changed (e.g. dev token flush, schema change), wipe everything
      const storedVersion = await AsyncStorage.getItem(KEYS.version);
      if (storedVersion !== AUTH_VERSION) {
        await clearPersistedAuth();
        await AsyncStorage.setItem(KEYS.version, AUTH_VERSION);
        console.log('Auth version changed — cleared stale session, redirecting to login');
        set({ user: null, accessToken: null, refreshToken: null, isAuthenticated: false, isLoading: false });
        return;
      }
      // ─────────────────────────────────────────────────────────────────────────

      const userStr = await AsyncStorage.getItem(KEYS.user);
      const { access, refresh } = await loadTokens();

      const user = userStr ? JSON.parse(userStr) : null;

      // Nothing stored → go to login
      if (!user || !access || !refresh) {
        set({ isLoading: false });
        return;
      }

      // Load the stored tokens first so that, if the access token has expired,
      // the API client refreshes it through the shared refreshAccessToken.
      set({ accessToken: access, refreshToken: refresh });

      // Validate the stored session with the backend
      try {
        const { apiClient } = require('@/services/api/client');
        await apiClient.get('/users/profile', { timeout: 5000 });

        // Session is valid (possibly after a refresh) → restore auth
        set({ user, isAuthenticated: true });
      } catch (validationError: any) {
        // refreshAccessToken clears the refresh token when the server rejects it.
        const sessionRejected = !get().refreshToken || validationError?.response?.status === 401;

        if (sessionRejected) {
          console.warn('Session expired — redirecting to login');
          await clearPersistedAuth();
          set({ user: null, accessToken: null, refreshToken: null, isAuthenticated: false });
        } else {
          // Backend unreachable or erroring → trust cached data
          console.warn('Could not validate session right now — using cached auth');
          set({ user, isAuthenticated: true });
        }
      }
    } catch (error) {
      console.error('Failed to load persisted auth:', error);
      // On any unexpected error, clear auth for safety
      await clearPersistedAuth();
    } finally {
      set({ isLoading: false });
    }
  },

  clearError: () => set({ error: null }),
  setError: (error) => set({ error }),
}));
