import { apiClient } from './client';
import {
  SendOTPRequest,
  VerifyOTPRequest,
  LoginRequest,
  SetupProfileRequest,
  ChangePasswordRequest,
  AuthResponse,
  BackendUser,
} from '@/types/api';

export const authService = {
  // Step 1: Request OTP (Signup or Login via OTP)
  sendOTP: async (phone: string, type?: 'signup' | 'login'): Promise<{ message: string }> => {
    const url = type ? `/auth/otp/send?type=${type}` : '/auth/otp/send';
    const response = await apiClient.post<{ message: string }>(url, { phone } as SendOTPRequest);
    return response.data;
  },

  // Step 2: Verify OTP - creates/logs in user and returns JWT tokens
  verifyOTP: async (phone: string, otp: string): Promise<AuthResponse> => {
    const response = await apiClient.post<AuthResponse>('/auth/otp/verify', { phone, otp } as VerifyOTPRequest);
    return response.data;
  },

  // Step 3 (optional): Setup name, email, password after OTP
  setupProfile: async (data: SetupProfileRequest, accessToken: string): Promise<{ message: string; user: BackendUser }> => {
    const response = await apiClient.post<{ message: string; user: BackendUser }>(
      '/auth/password/setup',
      data,
      { headers: { Authorization: `Bearer ${accessToken}` } }
    );
    return response.data;
  },

  async updateProfile(data: Partial<SetupProfileRequest>, token: string) {
    const response = await apiClient.put('/users/profile', data, {
      headers: { Authorization: `Bearer ${token}` }
    });
    return response.data;
  },

  changePassword: async (data: ChangePasswordRequest): Promise<{ message: string }> => {
    const response = await apiClient.post<{ message: string }>('/auth/password/change', data);
    return response.data;
  },

  // Direct Login with phone + password (for returning users who set up password)
  login: async (credentials: LoginRequest): Promise<AuthResponse> => {
    const response = await apiClient.post<AuthResponse>('/auth/login', credentials);
    return response.data;
  },

  resetPassword: async (phone: string, otp: string, newPassword: string): Promise<{ message: string }> => {
    const response = await apiClient.post<{ message: string }>('/auth/password/reset', {
      phone,
      otp,
      new_password: newPassword,
    });
    return response.data;
  },

  logout: async (refreshToken: string | null): Promise<void> => {
    await apiClient.post('/auth/logout', refreshToken ? { refresh_token: refreshToken } : {});
  },

  // Refresh access token using refresh token
  refreshToken: async (refreshToken: string): Promise<{ access: string; refresh: string }> => {
    const response = await apiClient.post<{ tokens: { access: string; refresh: string } }>(
      '/auth/refresh',
      { refresh_token: refreshToken }
    );
    return response.data.tokens;
  },
};
