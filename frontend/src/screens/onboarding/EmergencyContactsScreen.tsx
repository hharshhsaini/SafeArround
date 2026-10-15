import React, { useState } from 'react';
import { View, StyleSheet, ScrollView, Alert as RNAlert } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Text, Checkbox } from 'react-native-paper';
import { useNavigation } from '@react-navigation/native';
import { useForm, Controller } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { Button, Input, Card, ListItem, Alert } from '@/components/common';
import { PhoneInput } from '@/components/forms';
import { emergencyContactSchema, EmergencyContactFormData } from '@/utils/validation';
import { EmergencyContact } from '@/types/models';
import { colors } from '@/theme/colors';
import { spacing } from '@/theme/spacing';
import { fontSizes } from '@/theme/typography';
import { profileApiService } from '@/services/api/profileApiService';
import { DeviceContactsPickerModal } from '@/components/profile/DeviceContactsPickerModal';
import { DeviceContactOption } from '@/services/deviceContactsService';
import { normalizePhoneNumber } from '@/utils/phone';

const MAX_CONTACTS = 5;

/**
 * Emergency contacts setup screen
 * Allows adding 1-5 emergency contacts during onboarding, typed in or chosen
 * from the phone's contacts
 */
export const EmergencyContactsScreen = () => {
  const navigation = useNavigation();
  const [contacts, setContacts] = useState<EmergencyContact[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [showPicker, setShowPicker] = useState(false);
  const [isPrimary, setIsPrimary] = useState(false);

  const {
    control,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<EmergencyContactFormData>({
    resolver: zodResolver(emergencyContactSchema),
  });

  const onImportContacts = (selected: DeviceContactOption[]) => {
    const imported: EmergencyContact[] = selected
      .slice(0, MAX_CONTACTS - contacts.length)
      .map((contact, index) => ({
        id: Date.now() + index,
        userId: 0,
        name: contact.name,
        phoneNumber: contact.normalizedPhone,
        phone: contact.normalizedPhone,
        relationship: 'Trusted contact',
        isPrimary: contacts.length === 0 && index === 0,
        createdAt: new Date().toISOString(),
      }));
    setContacts([...contacts, ...imported]);
  };

  const onAddContact = (data: EmergencyContactFormData) => {
    if (contacts.length >= MAX_CONTACTS) {
      return;
    }

    const newContact: EmergencyContact = {
      id: Date.now(),
      userId: 0,
      name: data.name,
      phoneNumber: data.phone,
      phone: data.phone,
      relationship: data.relationship,
      isPrimary: contacts.length === 0 ? true : isPrimary,
      createdAt: new Date().toISOString(),
    };

    setContacts([...contacts, newContact]);
    setShowForm(false);
    setIsPrimary(false);
    reset();
  };

  const removeContact = (contactId: number) => {
    setContacts(contacts.filter((c) => c.id !== contactId));
  };

  const handleContinue = async () => {
    // Save contacts to backend. A 409 means the contact was already saved by
    // an earlier attempt, so retrying after a partial failure is safe.
    const failed: string[] = [];
    for (const contact of contacts) {
      try {
        await profileApiService.addContact({
          name: contact.name,
          phone: contact.phoneNumber,
          relationship: contact.relationship,
          is_priority: contact.isPrimary,
        });
      } catch (error: any) {
        if (error?.response?.status !== 409) {
          console.error('Failed to save contact:', error);
          failed.push(contact.name);
        }
      }
    }

    if (failed.length > 0) {
      RNAlert.alert(
        'Error',
        `Failed to save: ${failed.join(', ')}. Check the number and try again.`
      );
      return;
    }
    navigation.navigate('OnboardingTutorial' as never);
  };

  const handleSkip = () => {
    navigation.navigate('OnboardingTutorial' as never);
  };

  return (
    <SafeAreaView style={styles.container} edges={['top', 'bottom']}>
      <ScrollView style={styles.container} contentContainerStyle={styles.content}>
        <View style={styles.header}>
        <Text style={styles.title}>Emergency Contacts</Text>
        <Text style={styles.subtitle}>
          Add trusted contacts who will be notified during emergencies (1-5 contacts)
        </Text>
      </View>

        {contacts.length === 0 && !showForm && (
          <Alert
            type="info"
            message="Add at least one emergency contact to continue"
          />
        )}

        {contacts.map((contact) => (
        <Card key={contact.id} variant="outlined" padding="md" style={styles.contactCard}>
          <ListItem
            title={contact.name}
            subtitle={`${contact.relationship} • ${contact.phoneNumber}`}
            leftIcon="person"
            rightComponent={
              contact.isPrimary ? (
                <Text style={styles.primaryBadge}>Primary</Text>
              ) : undefined
            }
            swipeActions={{
              right: {
                icon: 'delete',
                color: colors.error,
                onPress: () => removeContact(contact.id),
              },
            }}
          />
        </Card>
      ))}

        {showForm && (
        <Card variant="elevated" padding="lg" style={styles.formCard}>
          <Text style={styles.formTitle}>Add Emergency Contact</Text>

          <Controller
            control={control}
            name="name"
            render={({ field: { onChange, value } }) => (
              <Input
                label="Full Name"
                value={value}
                onChangeText={onChange}
                placeholder="John Doe"
                error={errors.name?.message}
                leftIcon="person"
                autoFocus
              />
            )}
          />

          <Controller
            control={control}
            name="phone"
            render={({ field: { onChange, value } }) => (
              <PhoneInput
                label="Phone Number"
                value={value}
                onChangeText={onChange}
                error={errors.phone?.message}
              />
            )}
          />

          <Controller
            control={control}
            name="relationship"
            render={({ field: { onChange, value } }) => (
              <Input
                label="Relationship"
                value={value}
                onChangeText={onChange}
                placeholder="e.g., Mother, Friend, Spouse"
                error={errors.relationship?.message}
                leftIcon="people"
              />
            )}
          />

          {contacts.length > 0 && (
            <View style={styles.primaryContainer}>
              <Checkbox
                status={isPrimary ? 'checked' : 'unchecked'}
                onPress={() => setIsPrimary(!isPrimary)}
                color={colors.primary}
              />
              <Text style={styles.primaryText}>Mark as primary contact</Text>
            </View>
          )}

          <View style={styles.formActions}>
            <Button
              variant="outline"
              size="medium"
              onPress={() => {
                setShowForm(false);
                reset();
              }}
              style={styles.cancelButton}
            >
              Cancel
            </Button>
            <Button
              variant="primary"
              size="medium"
              onPress={handleSubmit(onAddContact)}
              style={styles.saveButton}
            >
              Save Contact
            </Button>
          </View>
        </Card>
      )}

        {!showForm && contacts.length < MAX_CONTACTS && (
        <>
          <Button
            variant="outline"
            size="large"
            fullWidth
            icon="contacts"
            onPress={() => setShowPicker(true)}
            style={styles.addButton}
          >
            Choose from Phone Contacts
          </Button>
          <Button
            variant="ghost"
            size="large"
            fullWidth
            icon="add"
            onPress={() => setShowForm(true)}
            style={styles.addButton}
          >
            Type a Number Instead
          </Button>
        </>
      )}

        <View style={styles.footer}>
        <Button
          variant="primary"
          size="large"
          fullWidth
          onPress={handleContinue}
          disabled={contacts.length === 0}
        >
          Save & Continue
        </Button>

        <Button
          variant="ghost"
          size="medium"
          fullWidth
          onPress={handleSkip}
        >
          Skip for now
        </Button>
        </View>
      </ScrollView>

      <DeviceContactsPickerModal
        visible={showPicker}
        existingPhones={contacts.map((contact) => normalizePhoneNumber(contact.phoneNumber || contact.phone || ''))}
        maxSelectable={MAX_CONTACTS - contacts.length}
        onClose={() => setShowPicker(false)}
        onImport={onImportContacts}
      />
    </SafeAreaView>
  );
};

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.surface,
  },
  content: {
    padding: spacing['2xl'],
    paddingBottom: spacing['4xl'],
  },
  header: {
    marginTop: spacing.xl,
    marginBottom: spacing['2xl'],
  },
  title: {
    fontSize: fontSizes['3xl'],
    fontWeight: '700',
    color: colors.textPrimary,
    marginBottom: spacing.sm,
  },
  subtitle: {
    fontSize: fontSizes.md,
    color: colors.textSecondary,
    lineHeight: 22,
  },
  contactCard: {
    marginBottom: spacing.md,
  },
  primaryBadge: {
    fontSize: fontSizes.xs,
    color: colors.primary,
    fontWeight: '600',
    backgroundColor: `${colors.primary}20`,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
    borderRadius: 4,
  },
  formCard: {
    marginBottom: spacing.lg,
  },
  formTitle: {
    fontSize: fontSizes.xl,
    fontWeight: '600',
    color: colors.textPrimary,
    marginBottom: spacing.lg,
  },
  primaryContainer: {
    flexDirection: 'row',
    alignItems: 'center',
    marginBottom: spacing.lg,
  },
  primaryText: {
    fontSize: fontSizes.sm,
    color: colors.textPrimary,
    marginLeft: spacing.sm,
  },
  formActions: {
    flexDirection: 'row',
    gap: spacing.md,
  },
  cancelButton: {
    flex: 1,
  },
  saveButton: {
    flex: 1,
  },
  addButton: {
    marginBottom: spacing.xl,
  },
  footer: {
    marginTop: spacing.xl,
  },
});
