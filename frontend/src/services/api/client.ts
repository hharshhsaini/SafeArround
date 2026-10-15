import axios, { AxiosError, InternalAxiosRequestConfig } from 'axios';
import { API_URL } from '@/config/env';

export const apiClient = axios.create({
  baseURL: API_URL,
  timeout: 10000,
  headers: {
    'Content-Type': 'application/json',
  },
});

// Request interceptor - Add JWT token
apiClient.interceptors.request.use(
  (config: InternalAxiosRequestConfig) => {
    const { useAuthStore } = require('@/store/authStore');
    const token = useAuthStore.getState().accessToken;
    if (token && config.headers) {
      config.headers.Authorization = `Bearer ${token}`;
    }
    return config;
  },
  (error) => Promise.reject(error)
);

// Response interceptor - Handle errors and token refresh
apiClient.interceptors.response.use(
  (response) => response,
  async (error: AxiosError) => {
    const originalRequest = error.config as InternalAxiosRequestConfig & { _retry?: boolean };

    // Handle 401 - Unauthorized, but ignore if the request was for auth endpoints to prevent infinite loops
    const isAuthEndpoint = originalRequest.url?.includes('auth/logout') || 
                           originalRequest.url?.includes('auth/refresh') || 
                           originalRequest.url?.includes('auth/login');
                           
    // Without a session there is nothing to refresh: let the 401 through so
    // callers such as OTP verification see the server's error.
    const { useAuthStore: authStoreForSession } = require('@/store/authStore');
    const hasSession = !!authStoreForSession.getState().refreshToken;

    if (error.response?.status === 401 && !originalRequest._retry && !isAuthEndpoint && hasSession) {
      originalRequest._retry = true;

      try {
        const { useAuthStore } = require('@/store/authStore');

        await useAuthStore.getState().refreshAccessToken();
        const newToken = useAuthStore.getState().accessToken;
        
        if (newToken && originalRequest.headers) {
          originalRequest.headers.Authorization = `Bearer ${newToken}`;
        }
        
        return apiClient(originalRequest);
      } catch (refreshError: any) {
        // Silently handle 401 refresh failures (expired tokens on startup)
        const is401 = refreshError?.response?.status === 401;
        if (!is401) {
          console.error('Token refresh failed:', refreshError);
        }
        const { useAuthStore } = require('@/store/authStore');
        useAuthStore.getState().setError(null); // Don't show error to user
        return Promise.reject(refreshError);
      }
    }

    // Handle network errors
    if (!error.response) {
      return Promise.reject({
        message: 'Network error. Please check your connection.',
        isNetworkError: true,
      });
    }

    return Promise.reject(error);
  }
);

// Retry logic with exponential backoff
export const retryRequest = async <T>(
  requestFn: () => Promise<T>,
  maxRetries = 3,
  baseDelay = 1000
): Promise<T> => {
  let lastError: any;

  for (let i = 0; i < maxRetries; i++) {
    try {
      return await requestFn();
    } catch (error) {
      lastError = error;
      if (i < maxRetries - 1) {
        const delay = baseDelay * Math.pow(2, i);
        await new Promise((resolve) => setTimeout(resolve, delay));
      }
    }
  }

  throw lastError;
};
