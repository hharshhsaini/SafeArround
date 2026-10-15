import React, { useState, useEffect } from 'react';
import { View, StyleSheet, ScrollView, Alert as RNAlert } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Text, Checkbox, ActivityIndicator } from 'react-native-paper';
import { useNavigation } from '@react-navigation/native';
import { useForm, Controller } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { MaterialIcons as Icon } from '@expo/vector-icons';
import { Button, Input, Card, ListItem, Alert } from '@/components/common';
import { PhoneInput } from '@/components/forms';
import { emergencyContactSchema, EmergencyContactFormData } from '@/utils/validation';
import { EmergencyContact } from '@/types/models';
import { colors } from '@/theme/colors';
import { spacing, borderRadius } from '@/theme/spacing';
import { fontSizes } from '@/theme/typography';
import { profileApiService } from '@/services/api/profileApiService';
import { useUserStore } from '@/store/userStore';
import { DeviceContactsPickerModal } from '@/components/profile/DeviceContactsPickerModal';
import { DeviceContactOption } from '@/services/deviceContactsService';
import { normalizePhoneNumber } from '@/utils/phone';

const MAX_CONTACTS = 5;

/**
 * Emergency contacts management screen
 * Allows viewing, adding (typed in or imported from the phone), and deleting emergency contacts
 */
export const EmergencyContactsManagementScreen = () => {
  const navigation = useNavigation();
  const { setEmergencyContacts } = useUserStore();
  const [contacts, setContacts] = useState<EmergencyContact[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [showPicker, setShowPicker] = useState(false);
  const [isPrimary, setIsPrimary] = useState(false);
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);

  const {
    control,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<EmergencyContactFormData>({
    resolver: zodResolver(emergencyContactSchema),
  });

  useEffect(() => {
    loadContacts();
  }, []);

  const loadContacts = async () => {
    try {
      setLoading(true);
      const data = await profileApiService.getContacts();
      // Map backend format to frontend format
      const mappedContacts = data.map(contact => ({
        ...contact,
        phoneNumber: contact.phone || contact.phoneNumber,
        isPrimary: contact.is_priority ?? contact.isPrimary,
      }));
      setContacts(mappedContacts);
      setEmergencyContacts(mappedContacts);
    } catch (error) {
      console.error('Failed to load contacts:', error);
      RNAlert.alert('Error', 'Failed to load emergency contacts');
    } finally {
      setLoading(false);
    }
  };

  const onImportContacts = async (selected: DeviceContactOption[]) => {
    const toAdd = selected.slice(0, MAX_CONTACTS - contacts.length);
    const failed: string[] = [];

    for (const [index, contact] of toAdd.entries()) {
      try {
        await profileApiService.addContact({
          name: contact.name,
          phone: contact.normalizedPhone,
          relationship: 'Trusted contact',
          is_priority: contacts.length === 0 && index === 0,
        });
      } catch (error) {
        console.error('Failed to import contact:', error);
        failed.push(contact.name);
      }
    }

    await loadContacts();
    if (failed.length > 0) {
      RNAlert.alert('Some contacts were not added', `Could not add: ${failed.join(', ')}`);
    } else {
      RNAlert.alert('Success', `${toAdd.length} emergency contact${toAdd.length === 1 ? '' : 's'} added`);
    }
  };

  const onAddContact = async (data: EmergencyContactFormData) => {
    if (contacts.length >= MAX_CONTACTS) {
      RNAlert.alert('Limit Reached', 'You can only have up to 5 emergency contacts');
      return;
    }

    try {
      setSubmitting(true);
      await profileApiService.addContact({
        name: data.name,
        phone: data.phone,
        relationship: data.relationship,
        is_priority: contacts.length === 0 ? true : isPrimary,
      });
      
      await loadContacts();
      setShowForm(false);
      setIsPrimary(false);
      reset();
      RNAlert.alert('Success', 'Emergency contact added successfully');
    } catch (error) {
      console.error('Failed to add contact:', error);
      RNAlert.alert('Error', 'Failed to add emergency contact');
    } finally {
      setSubmitting(false);
    }
  };

  const removeContact = async (contactId: number) => {
    RNAlert.alert(
      'Delete Contact',
      'Are you sure you want to delete this emergency contact?',
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Delete',
          style: 'destructive',
          onPress: async () => {
            try {
              await profileApiService.deleteContact(contactId);
              await loadContacts();
              RNAlert.alert('Success', 'Contact deleted successfully');
            } catch (error) {
              console.error('Failed to delete contact:', error);
              RNAlert.alert('Error', 'Failed to delete contact');
            }
          },
        },
      ]
    );
  };

  if (loading) {
    return (
      <SafeAreaView style={styles.container} edges={['top', 'bottom']}>
        <View style={styles.loadingContainer}>
          <ActivityIndicator size="large" color={colors.primary} />
        </View>
      </SafeAreaView>
    );
  }

  return (
    <SafeAreaView style={styles.container} edges={['top', 'bottom']}>
      <View style={styles.header}>
        <Icon
          name="arrow-back"
          size={24}
          color={colors.textPrimary}
          onPress={() => navigation.goBack()}
        />
        <Text style={styles.headerTitle}>Emergency Contacts</Text>
        <View style={{ width: 24 }} />
      </View>

      <ScrollView style={styles.container} contentContainerStyle={styles.content}>
        <View style={styles.infoCard}>
          <Icon name="info-outline" size={20} color={colors.primary} />
          <Text style={styles.infoText}>
            Add 1-5 trusted contacts who will be notified during emergencies
          </Text>
        </View>

        {contacts.length === 0 && !showForm && (
          <Alert
            type="info"
            message="No emergency contacts added yet. Add at least one contact for safety."
          />
        )}

        {contacts.map((contact) => (
          <Card key={contact.id} variant="outlined" padding="md" style={styles.contactCard}>
            <View style={styles.contactContent}>
              <View style={styles.contactIcon}>
                <Icon name="person" size={24} color={colors.primary} />
              </View>
              <View style={styles.contactInfo}>
                <View style={styles.contactHeader}>
                  <Text style={styles.contactName}>{contact.name}</Text>
                  {contact.isPrimary && (
                    <View style={styles.primaryBadge}>
                      <Text style={styles.primaryText}>Primary</Text>
                    </View>
                  )}
                </View>
                <Text style={styles.contactDetail}>
                  {contact.relationship} • {contact.phoneNumber}
                </Text>
              </View>
              <Icon
                name="delete"
                size={24}
                color={colors.error}
                onPress={() => removeContact(contact.id)}
              />
            </View>
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
                <Text style={styles.primaryCheckboxText}>Mark as primary contact</Text>
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
                disabled={submitting}
              >
                Cancel
              </Button>
              <Button
                variant="primary"
                size="medium"
                onPress={handleSubmit(onAddContact)}
                style={styles.saveButton}
                loading={submitting}
                disabled={submitting}
              >
                Save Contact
              </Button>
            </View>
          </Card>
        )}

        {!showForm && contacts.length < MAX_CONTACTS && (
          <>
            <Button
              variant="primary"
              size="large"
              fullWidth
              icon="contacts"
              onPress={() => setShowPicker(true)}
              style={styles.addButton}
            >
              Choose from Phone Contacts
            </Button>
            <Button
              variant="outline"
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

        {contacts.length >= MAX_CONTACTS && (
          <Alert
            type="warning"
            message="Maximum limit reached. You can have up to 5 emergency contacts."
          />
        )}
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
    backgroundColor: colors.background,
  },
  loadingContainer: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: spacing.lg,
    backgroundColor: colors.surface,
    borderBottomWidth: 1,
    borderBottomColor: colors.border,
  },
  headerTitle: {
    fontSize: fontSizes.xl,
    fontWeight: '700',
    color: colors.textPrimary,
  },
  content: {
    padding: spacing.lg,
    paddingBottom: spacing['4xl'],
  },
  infoCard: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: `${colors.primary}10`,
    padding: spacing.md,
    borderRadius: borderRadius.md,
    marginBottom: spacing.lg,
    gap: spacing.sm,
  },
  infoText: {
    flex: 1,
    fontSize: fontSizes.sm,
    color: colors.textPrimary,
    lineHeight: 20,
  },
  contactCard: {
    marginBottom: spacing.md,
  },
  contactContent: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
  },
  contactIcon: {
    width: 48,
    height: 48,
    borderRadius: 24,
    backgroundColor: `${colors.primary}10`,
    justifyContent: 'center',
    alignItems: 'center',
  },
  contactInfo: {
    flex: 1,
  },
  contactHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    marginBottom: spacing.xs,
  },
  contactName: {
    fontSize: fontSizes.md,
    fontWeight: '600',
    color: colors.textPrimary,
  },
  primaryBadge: {
    backgroundColor: `${colors.primary}20`,
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    borderRadius: borderRadius.sm,
  },
  primaryText: {
    fontSize: fontSizes.xs,
    color: colors.primary,
    fontWeight: '600',
  },
  contactDetail: {
    fontSize: fontSizes.sm,
    color: colors.textSecondary,
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
  primaryCheckboxText: {
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
    marginBottom: spacing.lg,
  },
});
