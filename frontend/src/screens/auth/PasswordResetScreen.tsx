import React, { useState } from 'react';
import { View, StyleSheet, KeyboardAvoidingView, Platform, ScrollView } from 'react-native';
import { Text } from 'react-native-paper';
import { useNavigation } from '@react-navigation/native';
import { Button, Input, Alert } from '@/components/common';
import { useAuth } from '@/hooks/useAuth';
import { colors } from '@/theme/colors';
import { spacing, borderRadius, shadows } from '@/theme/spacing';
import { fontSizes } from '@/theme/typography';

type ResetStep = 'phone' | 'otp' | 'password';

/**
 * Password reset with 3-step OTP flow matching backend:
 * Step 1: Enter phone → POST /auth/otp/send
 * Step 2: Enter OTP   → kept on the device (a code can only be checked once)
 * Step 3: New password → POST /auth/password/reset with phone, OTP and password
 * The user then signs in with the new password.
 */
export const PasswordResetScreen = () => {
  const navigation = useNavigation();
  const { sendOTP, resetPassword, error, clearError } = useAuth();

  const [step, setStep] = useState<ResetStep>('phone');
  const [phone, setPhone] = useState('');
  const [otp, setOtp] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [success, setSuccess] = useState(false);

  const handleSendOTP = async () => {
    if (!phone) return;
    try {
      setIsSubmitting(true);
      clearError();
      await sendOTP(phone);
      setStep('otp');
    } catch (err) {
      // error handled by store
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleContinueWithOTP = () => {
    if (otp.length !== 6) return;
    clearError();
    setStep('password');
  };

  const handleResetPassword = async () => {
    if (newPassword.length < 8 || newPassword !== confirmPassword) return;
    try {
      setIsSubmitting(true);
      clearError();
      await resetPassword(phone, otp, newPassword);
      setSuccess(true);
      setTimeout(() => navigation.navigate('Login' as never), 2000);
    } catch (err: any) {
      // The code is only checked now, so send the user back to re-enter it.
      if (err?.response?.status === 401) {
        setOtp('');
        setStep('otp');
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <KeyboardAvoidingView
      style={styles.container}
      behavior={Platform.OS === 'ios' ? 'padding' : 'height'}
    >
      <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
        <View style={styles.header}>
          <Text style={styles.title}>Reset Password</Text>
          <Text style={styles.subtitle}>
            {step === 'phone' && 'Enter your phone number to receive a verification code'}
            {step === 'otp' && `Enter the 6-digit code sent to ${phone}`}
            {step === 'password' && 'Create a new password for your account'}
          </Text>
        </View>

        {error && <Alert type="error" message={error} onDismiss={clearError} />}
        {success && <Alert type="success" message="Password reset successful! Redirecting..." />}

        <View style={styles.form}>

          {step === 'phone' && (
            <>
              <Input
                label="Phone Number"
                value={phone}
                onChangeText={setPhone}
                placeholder="+919119759509"
                leftIcon="phone"
                keyboardType="phone-pad"
                autoFocus
              />
              <Button variant="primary" size="large" fullWidth onPress={handleSendOTP} loading={isSubmitting}>
                Send OTP
              </Button>
            </>
          )}

          {step === 'otp' && (
            <>
              <Input
                label="Verification Code"
                value={otp}
                onChangeText={setOtp}
                placeholder="Enter 6-digit code"
                maxLength={6}
                leftIcon="lock"
                keyboardType="number-pad"
                autoFocus
              />
              <Button variant="primary" size="large" fullWidth onPress={handleContinueWithOTP} disabled={otp.length !== 6}>
                Continue
              </Button>
              <Button variant="ghost" size="medium" fullWidth onPress={handleSendOTP}>
                Resend Code
              </Button>
            </>
          )}

          {step === 'password' && (
            <>
              <Input
                type="password"
                label="New Password"
                value={newPassword}
                onChangeText={setNewPassword}
                placeholder="Enter new password (min 8 chars)"
                leftIcon="lock"
                autoFocus
              />
              <Input
                type="password"
                label="Confirm Password"
                value={confirmPassword}
                onChangeText={setConfirmPassword}
                placeholder="Re-enter new password"
                leftIcon="lock"
              />
              {confirmPassword.length > 0 && newPassword !== confirmPassword && (
                <Text style={styles.mismatch}>Passwords do not match</Text>
              )}
              <Button
                variant="primary"
                size="large"
                fullWidth
                onPress={handleResetPassword}
                loading={isSubmitting}
                disabled={newPassword !== confirmPassword || newPassword.length < 8}
              >
                Reset Password
              </Button>
            </>
          )}

          <Button variant="ghost" size="medium" fullWidth onPress={() => navigation.goBack()}>
            Back to Login
          </Button>

        </View>
      </ScrollView>
    </KeyboardAvoidingView>
  );
};

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: colors.background },
  content: { flexGrow: 1, padding: spacing['2xl'], justifyContent: 'center' },
  header: { marginBottom: spacing['2xl'] },
  title: {
    fontSize: fontSizes['3xl'],
    fontWeight: '700',
    color: colors.textPrimary,
    marginBottom: spacing.sm,
  },
  subtitle: { fontSize: fontSizes.md, color: colors.textSecondary, lineHeight: 22 },
  form: {
    backgroundColor: colors.surface,
    borderRadius: borderRadius.xl,
    padding: spacing.xl,
    marginBottom: spacing['2xl'],
    ...shadows.medium,
  },
  mismatch: {
    fontSize: fontSizes.xs,
    color: colors.error,
    marginBottom: spacing.md,
  },
});
