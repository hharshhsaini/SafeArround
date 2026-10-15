import React, { useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  Linking,
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { MaterialIcons as Icon } from '@expo/vector-icons';
import { Button } from '@/components/common';
import {
  ContactsPermissionError,
  DeviceContactOption,
  deviceContactsService,
} from '@/services/deviceContactsService';
import { colors } from '@/theme/colors';
import { borderRadius, shadows, spacing } from '@/theme/spacing';
import { fontSizes } from '@/theme/typography';

interface DeviceContactsPickerModalProps {
  visible: boolean;
  /** Normalized (E.164) numbers already saved, hidden from the list. */
  existingPhones?: string[];
  /** How many more contacts the user may add. */
  maxSelectable?: number;
  onClose: () => void;
  onImport: (contacts: DeviceContactOption[]) => Promise<void> | void;
}

export const DeviceContactsPickerModal: React.FC<DeviceContactsPickerModalProps> = ({
  visible,
  existingPhones = [],
  maxSelectable,
  onClose,
  onImport,
}) => {
  const [loading, setLoading] = useState(false);
  const [importing, setImporting] = useState(false);
  const [search, setSearch] = useState('');
  const [contacts, setContacts] = useState<DeviceContactOption[]>([]);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [loadError, setLoadError] = useState('');
  const existingPhoneSet = useMemo(() => new Set(existingPhones), [existingPhones]);

  useEffect(() => {
    if (!visible) {
      setSearch('');
      setSelectedIds(new Set());
      setLoadError('');
      return;
    }

    let isCancelled = false;
    const loadContacts = async () => {
      setLoading(true);
      setLoadError('');
      try {
        const nextContacts = await deviceContactsService.getSelectableContacts();
        if (!isCancelled) {
          setContacts(nextContacts);
        }
      } catch (error) {
        if (!isCancelled) {
          setContacts([]);
          const message =
            error instanceof Error
              ? error.message
              : 'We could not load your phone contacts right now.';
          setLoadError(message);

          if (error instanceof ContactsPermissionError && error.canAskAgain === false) {
            Alert.alert(
              'Contacts permission needed',
              message,
              [
                { text: 'Not now', style: 'cancel' },
                { text: 'Open settings', onPress: () => void Linking.openSettings() },
              ],
            );
          }
        }
      } finally {
        if (!isCancelled) {
          setLoading(false);
        }
      }
    };

    void loadContacts();

    return () => {
      isCancelled = true;
    };
  }, [visible]);

  const filteredContacts = useMemo(() => {
    const normalizedSearch = search.trim().toLowerCase();
    return contacts.filter((contact) => {
      if (existingPhoneSet.has(contact.normalizedPhone)) {
        return false;
      }
      if (!normalizedSearch) {
        return true;
      }

      return (
        contact.name.toLowerCase().includes(normalizedSearch) ||
        contact.phoneNumber.toLowerCase().includes(normalizedSearch)
      );
    });
  }, [contacts, existingPhoneSet, search]);

  const toggleSelection = (contactId: string) => {
    if (!selectedIds.has(contactId) && maxSelectable !== undefined && selectedIds.size >= maxSelectable) {
      Alert.alert(
        'Contact limit reached',
        `You can add ${maxSelectable} more emergency contact${maxSelectable === 1 ? '' : 's'}.`,
      );
      return;
    }

    setSelectedIds((previous) => {
      const next = new Set(previous);
      if (next.has(contactId)) {
        next.delete(contactId);
      } else {
        next.add(contactId);
      }
      return next;
    });
  };

  const handleImport = async () => {
    // Pick from all contacts, not just those matching the current search, so
    // a selection made before searching again is not dropped.
    const selectedContacts = contacts.filter((contact) => selectedIds.has(contact.id));
    if (selectedContacts.length === 0) {
      onClose();
      return;
    }

    setImporting(true);
    try {
      await onImport(selectedContacts);
      onClose();
    } finally {
      setImporting(false);
    }
  };

  return (
    <Modal visible={visible} animationType="slide" transparent onRequestClose={onClose}>
      <View style={styles.backdrop}>
        <SafeAreaView style={styles.sheet} edges={['top', 'bottom']}>
          <View style={styles.header}>
            <View>
              <Text style={styles.title}>Import from Contacts</Text>
              <Text style={styles.subtitle}>
                Select trusted people from your phone book.
                {maxSelectable !== undefined ? ` You can add ${maxSelectable} more.` : ''}
              </Text>
            </View>
            <Pressable onPress={onClose} style={styles.closeButton}>
              <Icon name="close" size={24} color={colors.textPrimary} />
            </Pressable>
          </View>

          <View style={styles.searchRow}>
            <Icon name="search" size={20} color={colors.textSecondary} />
            <TextInput
              value={search}
              onChangeText={setSearch}
              placeholder="Search contacts"
              placeholderTextColor={colors.textSecondary}
              style={styles.searchInput}
            />
          </View>

          <ScrollView style={styles.list} contentContainerStyle={styles.listContent}>
            {loading ? (
              <View style={styles.emptyState}>
                <ActivityIndicator size="large" color={colors.primary} />
                <Text style={styles.emptyText}>Loading your contacts...</Text>
              </View>
            ) : filteredContacts.length === 0 ? (
              <View style={styles.emptyState}>
                <Icon name="contacts" size={36} color={colors.textSecondary} />
                <Text style={styles.emptyTitle}>No importable contacts found</Text>
                <Text style={styles.emptyText}>
                  {loadError || 'We only show contacts with phone numbers that are not already saved.'}
                </Text>
              </View>
            ) : (
              filteredContacts.map((contact) => {
                const selected = selectedIds.has(contact.id);
                return (
                  <Pressable
                    key={contact.id}
                    style={[styles.contactRow, selected && styles.contactRowSelected]}
                    onPress={() => toggleSelection(contact.id)}
                  >
                    <View style={styles.contactAvatar}>
                      <Text style={styles.contactInitials}>
                        {contact.name.slice(0, 2).toUpperCase()}
                      </Text>
                    </View>
                    <View style={styles.contactInfo}>
                      <Text style={styles.contactName}>{contact.name}</Text>
                      <Text style={styles.contactPhone}>{contact.phoneNumber}</Text>
                    </View>
                    <View style={[styles.checkCircle, selected && styles.checkCircleSelected]}>
                      {selected ? <Icon name="check" size={18} color={colors.surface} /> : null}
                    </View>
                  </Pressable>
                );
              })
            )}
          </ScrollView>

          <View style={styles.footer}>
            <Button
              variant="outline"
              size="medium"
              onPress={onClose}
              style={styles.footerButton}
              disabled={importing}
            >
              Cancel
            </Button>
            <Button
              variant="primary"
              size="medium"
              onPress={handleImport}
              style={styles.footerButton}
              disabled={importing || selectedIds.size === 0}
              loading={importing}
            >
              Import Selected
            </Button>
          </View>
        </SafeAreaView>
      </View>
    </Modal>
  );
};

const styles = StyleSheet.create({
  backdrop: {
    flex: 1,
    backgroundColor: 'rgba(15, 23, 42, 0.35)',
    justifyContent: 'center',
    padding: spacing.lg,
  },
  sheet: {
    maxHeight: '82%',
    backgroundColor: colors.surface,
    borderRadius: borderRadius.xl,
    overflow: 'hidden',
    ...shadows.large,
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.lg,
    paddingBottom: spacing.md,
  },
  title: {
    fontSize: fontSizes.xl,
    fontWeight: '700',
    color: colors.textPrimary,
  },
  subtitle: {
    marginTop: spacing.xs,
    color: colors.textSecondary,
  },
  closeButton: {
    width: 36,
    height: 36,
    borderRadius: 18,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.background,
  },
  searchRow: {
    marginHorizontal: spacing.lg,
    marginBottom: spacing.md,
    paddingHorizontal: spacing.md,
    height: 48,
    borderRadius: borderRadius.pill,
    backgroundColor: colors.background,
    flexDirection: 'row',
    alignItems: 'center',
  },
  searchInput: {
    flex: 1,
    marginLeft: spacing.sm,
    color: colors.textPrimary,
  },
  list: {
    maxHeight: 420,
  },
  listContent: {
    paddingHorizontal: spacing.lg,
    paddingBottom: spacing.lg,
  },
  emptyState: {
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: spacing['3xl'],
    gap: spacing.sm,
  },
  emptyTitle: {
    fontSize: fontSizes.lg,
    fontWeight: '600',
    color: colors.textPrimary,
  },
  emptyText: {
    textAlign: 'center',
    color: colors.textSecondary,
    paddingHorizontal: spacing.lg,
  },
  contactRow: {
    flexDirection: 'row',
    alignItems: 'center',
    padding: spacing.md,
    borderRadius: borderRadius.lg,
    backgroundColor: colors.background,
    marginBottom: spacing.sm,
  },
  contactRowSelected: {
    borderWidth: 1,
    borderColor: colors.primary,
    backgroundColor: `${colors.primary}10`,
  },
  contactAvatar: {
    width: 42,
    height: 42,
    borderRadius: 21,
    backgroundColor: `${colors.primary}20`,
    justifyContent: 'center',
    alignItems: 'center',
  },
  contactInitials: {
    color: colors.primary,
    fontWeight: '700',
  },
  contactInfo: {
    flex: 1,
    marginLeft: spacing.md,
  },
  contactName: {
    color: colors.textPrimary,
    fontWeight: '600',
  },
  contactPhone: {
    marginTop: 2,
    color: colors.textSecondary,
  },
  checkCircle: {
    width: 24,
    height: 24,
    borderRadius: 12,
    borderWidth: 1,
    borderColor: colors.border,
    alignItems: 'center',
    justifyContent: 'center',
  },
  checkCircleSelected: {
    backgroundColor: colors.primary,
    borderColor: colors.primary,
  },
  footer: {
    flexDirection: 'row',
    gap: spacing.md,
    padding: spacing.lg,
    borderTopWidth: 1,
    borderTopColor: colors.border,
  },
  footerButton: {
    flex: 1,
  },
});
