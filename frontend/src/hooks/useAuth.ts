import { useEffect } from 'react';
import { useAuthStore } from '@/store/authStore';

/**
 * Custom hook for authentication operations
 * Wraps authStore and provides auto-refresh token functionality
 */
export const useAuth = () => {
  const {
    user,
    accessToken,
    isAuthenticated,
    isLoading,
    error,
    sendOTP,
    verifyOTP,
    setupProfile,
    logIn,
    resetPassword,
    logOut,
    refreshAccessToken,
    clearError,
    setError,
  } = useAuthStore();

  // Persisted auth is loaded once at startup by initializeApp; loading it again
  // from every screen that uses this hook raced with in-flight token refreshes.

  // Auto-refresh token every 50 minutes (tokens expire in ~60-168 min)
  useEffect(() => {
    if (!isAuthenticated || !accessToken) return;

    const refreshInterval = setInterval(() => {
      // Failures are handled inside refreshAccessToken.
      refreshAccessToken().catch(() => undefined);
    }, 50 * 60 * 1000);

    return () => clearInterval(refreshInterval);
  }, [isAuthenticated, accessToken]);

  return {
    user,
    accessToken,
    isAuthenticated,
    isLoading,
    error,
    // OTP-based auth flow
    sendOTP,
    verifyOTP,
    setupProfile,
    // Direct login with phone + password
    logIn,
    resetPassword,
    logOut,
    clearError,
    setError,
  };
};
