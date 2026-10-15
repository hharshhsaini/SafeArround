import React, { useState } from 'react';
import { View, TextInput, StyleSheet, Pressable, Text } from 'react-native';
import { MaterialIcons as Icon } from '@expo/vector-icons';
import { colors } from '@/theme/colors';
import { spacing, borderRadius } from '@/theme/spacing';
import { fontSizes } from '@/theme/typography';

interface PhoneInputProps {
  value: string;
  onChangeText: (text: string) => void;
  label?: string;
  error?: string;
  countryCode?: string;
  onCountryCodeChange?: (code: string) => void;
}

/**
 * Phone input component with country code selector
 * Formats phone numbers and validates input
 */
export const PhoneInput: React.FC<PhoneInputProps> = ({
  value,
  onChangeText,
  label,
  error,
  // India by default, matching the backend: with +1 a 10-digit Indian number
  // was reformatted as "(987) 654-3210" and failed validation.
  countryCode = '+91',
  onCountryCodeChange,
}) => {
  const [isFocused, setIsFocused] = useState(false);
  const supportedCountryCodes = ['+91', '+1'];

  const formatPhoneNumber = (text: string): string => {
    const cleaned = text.replace(/\D/g, '');
    
    if (countryCode === '+1' && cleaned.length <= 10) {
      if (cleaned.length <= 3) return cleaned;
      if (cleaned.length <= 6) return `(${cleaned.slice(0, 3)}) ${cleaned.slice(3)}`;
      return `(${cleaned.slice(0, 3)}) ${cleaned.slice(3, 6)}-${cleaned.slice(6, 10)}`;
    }
    
    return cleaned;
  };

  const handleChangeText = (text: string) => {
    const formatted = formatPhoneNumber(text);
    onChangeText(formatted);
  };

  const getBorderColor = () => {
    if (error) return colors.error;
    if (isFocused) return colors.primary;
    return colors.border;
  };

  const handleCountryCodePress = () => {
    if (!onCountryCodeChange) {
      return;
    }

    const currentIndex = supportedCountryCodes.indexOf(countryCode);
    const nextIndex = currentIndex === -1 ? 0 : (currentIndex + 1) % supportedCountryCodes.length;
    onCountryCodeChange(supportedCountryCodes[nextIndex]);
  };

  return (
    <View style={styles.container}>
      {label && <Text style={styles.label}>{label}</Text>}
      
      <View style={[styles.inputContainer, { borderColor: getBorderColor() }]}>
        <Pressable
          style={styles.countryCodeButton}
          onPress={handleCountryCodePress}
        >
          <Text style={styles.countryCode}>{countryCode}</Text>
          <Icon name="arrow-drop-down" size={20} color={colors.textSecondary} />
        </Pressable>
        
        <View style={styles.divider} />
        
        <TextInput
          style={styles.input}
          value={value}
          onChangeText={handleChangeText}
          placeholder="(555) 123-4567"
          placeholderTextColor={colors.textSecondary}
          keyboardType="phone-pad"
          maxLength={countryCode === '+1' ? 14 : 15}
          onFocus={() => setIsFocused(true)}
          onBlur={() => setIsFocused(false)}
          accessibilityLabel={label || 'Phone number input'}
        />
      </View>
      
      {error && <Text style={styles.errorText}>{error}</Text>}
    </View>
  );
};

const styles = StyleSheet.create({
  container: {
    marginBottom: spacing.lg,
  },
  label: {
    fontSize: fontSizes.sm,
    color: colors.textPrimary,
    marginBottom: spacing.sm,
    fontWeight: '500',
  },
  inputContainer: {
    flexDirection: 'row',
    alignItems: 'center',
    borderWidth: 1,
    borderRadius: borderRadius.md,
    backgroundColor: colors.surface,
  },
  countryCodeButton: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.md,
  },
  countryCode: {
    fontSize: fontSizes.md,
    color: colors.textPrimary,
    fontWeight: '500',
  },
  divider: {
    width: 1,
    height: 24,
    backgroundColor: colors.border,
  },
  input: {
    flex: 1,
    fontSize: fontSizes.md,
    color: colors.textPrimary,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.md,
  },
  errorText: {
    fontSize: fontSizes.xs,
    color: colors.error,
    marginTop: spacing.xs,
  },
});
