import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert, ScrollView, Share, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Text } from 'react-native-paper';
import { useNavigation, useRoute } from '@react-navigation/native';
import { MaterialIcons as Icon } from '@expo/vector-icons';
import { Button, Input, Avatar } from '@/components/common';
import { alertService } from '@/services/api/alertService';
import { feedbackService } from '@/services/api/feedbackService';
import { useAlertStore } from '@/store/alertStore';
import { AlertDetails } from '@/types/models';
import { colors } from '@/theme/colors';
import { spacing } from '@/theme/spacing';
import { fontSizes } from '@/theme/typography';
import { formatDateTime } from '@/utils/formatters';

const formatDuration = (durationSeconds: number) => {
  const safeSeconds = Math.max(0, durationSeconds);
  const minutes = Math.floor(safeSeconds / 60);
  const seconds = safeSeconds % 60;
  return `${minutes}:${seconds.toString().padStart(2, '0')}`;
};

const formatDistance = (distanceMeters: number) => {
  if (distanceMeters >= 1000) {
    return `${(distanceMeters / 1000).toFixed(1)} km`;
  }
  return `${Math.round(distanceMeters)} m`;
};

const formatEta = (etaSeconds: number) => {
  const minutes = Math.max(1, Math.round(etaSeconds / 60));
  return `${minutes} min ETA`;
};

export const EmergencyResolutionScreen = () => {
  const navigation = useNavigation();
  const route = useRoute<any>();
  const alertHistory = useAlertStore((state) => state.alertHistory);
  const fallbackAlertId = alertHistory.find((alert) => alert.status === 'resolved')?.id;
  const alertId = route.params?.alertId ?? fallbackAlertId;

  const [details, setDetails] = useState<AlertDetails | null>(null);
  const [loading, setLoading] = useState(true);
  const [rating, setRating] = useState(0);
  const [summaryNote, setSummaryNote] = useState('');
  const [feedback, setFeedback] = useState('');
  const [wasHelpful, setWasHelpful] = useState<boolean | null>(null);
  const [isClosing, setIsClosing] = useState(false);

  const loadDetails = useCallback(async () => {
    if (!alertId) {
      setLoading(false);
      return;
    }

    try {
      setLoading(true);
      const nextDetails = await alertService.getAlertDetails(alertId);
      setDetails(nextDetails);
    } catch (error) {
      console.warn('Failed to load resolved alert details:', error);
    } finally {
      setLoading(false);
    }
  }, [alertId]);

  useEffect(() => {
    loadDetails();
  }, [loadDetails]);

  const reportText = useMemo(() => {
    if (!details) {
      return '';
    }

    const lines = [
      'SafeAround Incident Report',
      `Alert ID: ${details.incidentReport.alertId}`,
      `Requester User ID: ${details.incidentReport.requesterUserId}`,
      `Status: ${details.incidentReport.status}`,
      `Started: ${formatDateTime(details.incidentReport.createdAt)}`,
      `Ended: ${formatDateTime(details.incidentReport.endedAt)}`,
      `Duration: ${formatDuration(details.durationSeconds)}`,
      `Max search radius: ${details.incidentReport.maxRadiusReached}m`,
      `Users notified: ${details.incidentReport.usersNotified}`,
      `Responders: ${details.respondersCount}`,
      `Emergency services: ${details.emergencyServicesStatus}`,
    ];

    if (details.incidentReport.responderUserIds.length > 0) {
      lines.push(`Responder User IDs: ${details.incidentReport.responderUserIds.join(', ')}`);
    }

    if (details.responders.length > 0) {
      lines.push('', 'Responders');
      details.responders.forEach((responder, index) => {
        lines.push(
          `${index + 1}. ${responder.name} | ${formatDistance(responder.distanceMeters)} | ${formatEta(responder.etaSeconds)} | ${responder.responseStatus}`,
        );
      });
    }

    return lines.join('\n');
  }, [details]);

  const handleSubmitRating = async () => {
    try {
      setIsClosing(true);

      // Submit feedback if user provided any
      if (rating > 0 || summaryNote.trim() || feedback.trim() || wasHelpful !== null) {
        // Rate the first responder only if the user actually picked stars;
        // otherwise the default below would count as a 3-star rating for them.
        const responderId = rating > 0 && details?.responders?.[0]?.userId
          ? parseInt(details.responders[0].userId, 10)
          : undefined;

        await feedbackService.submitFeedback({
          alertId: alertId!,
          responderId,
          rating: rating || 3, // Default to 3 if not provided
          feedback: feedback.trim() || undefined,
          summaryNote: summaryNote.trim() || undefined,
          wasHelpful: wasHelpful ?? undefined,
          feedbackType: 'resolution',
        });

        console.log('Feedback submitted successfully');
      }

      navigation.navigate('EmergencyDashboard' as never);
    } catch (error) {
      console.error('Failed to submit feedback:', error);
      Alert.alert(
        'Feedback Error',
        'We could not save your feedback, but your incident is still closed. You can continue.',
        [
          { text: 'OK', onPress: () => navigation.navigate('EmergencyDashboard' as never) }
        ]
      );
    } finally {
      setIsClosing(false);
    }
  };

  const handleShareReport = async () => {
    if (!reportText) {
      return;
    }

    try {
      await Share.share({
        title: 'SafeAround Incident Report',
        message: reportText,
      });
    } catch (error) {
      Alert.alert('Report unavailable', 'We could not prepare the incident report right now.');
    }
  };

  const summaryRows = useMemo(() => {
    if (!details) {
      return [];
    }

    return [
      {
        icon: 'schedule',
        label: 'Duration',
        value: formatDuration(details.durationSeconds),
      },
      {
        icon: 'notifications-active',
        label: 'People alerted',
        value: String(details.incidentReport.usersNotified),
      },
      {
        icon: 'my-location',
        label: 'Max radius reached',
        value: `${details.incidentReport.maxRadiusReached || details.incidentReport.currentRadius}m`,
      },
      {
        icon: 'people',
        label: 'Responders',
        value: String(details.respondersCount),
      },
      {
        icon: 'local-police',
        label: 'Emergency Services',
        value: details.emergencyServicesStatus || 'Not contacted',
      },
    ];
  }, [details]);

  if (loading) {
    return (
      <SafeAreaView style={styles.container} edges={['top', 'bottom']}>
        <View style={styles.centeredState}>
          <Text style={styles.loadingText}>Loading incident summary...</Text>
        </View>
      </SafeAreaView>
    );
  }

  if (!details) {
    return (
      <SafeAreaView style={styles.container} edges={['top', 'bottom']}>
        <View style={styles.centeredState}>
          <Text style={styles.loadingText}>We could not load this alert summary.</Text>
          <Button variant="outline" onPress={() => navigation.goBack()}>
            Go Back
          </Button>
        </View>
      </SafeAreaView>
    );
  }

  return (
    <SafeAreaView style={styles.container} edges={['top', 'bottom']}>
      <ScrollView
        style={styles.container}
        contentContainerStyle={styles.content}
        showsVerticalScrollIndicator={false}
      >
        <View style={styles.header}>
          <View style={styles.iconContainer}>
            <Icon name="check-circle" size={80} color={colors.success} />
          </View>
          <Text style={styles.title}>You're Safe!</Text>
          <Text style={styles.subtitle}>
            Your SOS is closed. Here is a clear summary of what happened during the alert.
          </Text>
        </View>

        <View style={styles.sectionCard}>
          <Text style={styles.cardTitle}>Alert Summary</Text>
          {summaryRows.map((row, index) => (
            <View
              key={row.label}
              style={[
                styles.summaryRow,
                index === summaryRows.length - 1 && styles.summaryRowLast,
              ]}
            >
              <Icon name={row.icon as any} size={20} color={colors.textSecondary} />
              <Text style={styles.summaryLabel}>{row.label}</Text>
              <Text style={styles.summaryValue}>{row.value}</Text>
            </View>
          ))}
        </View>

        {details.responders.length > 0 && (
          <View style={styles.sectionCard}>
            <Text style={styles.cardTitle}>Responders Who Helped</Text>
            {details.responders.map((responder) => (
              <View key={responder.userId} style={styles.responderItem}>
                <Avatar name={responder.name} size="medium" />
                <View style={styles.responderInfo}>
                  <Text style={styles.responderName}>{responder.name}</Text>
                  <Text style={styles.responderMeta}>
                    {formatDistance(responder.distanceMeters)} • {formatEta(responder.etaSeconds)}
                  </Text>
                </View>
              </View>
            ))}
          </View>
        )}

        {details.responders.length === 0 && (
          <View style={styles.sectionCard}>
            <Text style={styles.cardTitle}>Responder status</Text>
            <Text style={styles.emptyStateText}>
              No verified responder accepted this SOS before it was closed. Your incident report still includes the alert timeline and notification coverage.
            </Text>
          </View>
        )}

        <View style={styles.sectionCard}>
          <Text style={styles.cardTitle}>Optional feedback</Text>
          <Text style={styles.feedbackHint}>
            You can close this incident immediately, or leave a quick rating to help improve SOS handling.
          </Text>

          <View style={styles.starsContainer}>
            {[1, 2, 3, 4, 5].map((star) => (
              <Icon
                key={star}
                name={star <= rating ? 'star' : 'star-border'}
                size={40}
                color={star <= rating ? '#FDD835' : colors.border}
                onPress={() => setRating(star)}
              />
            ))}
          </View>

          <Input
            label="What happened?"
            value={summaryNote}
            onChangeText={setSummaryNote}
            placeholder="Example: Two nearby responders reached me within 3 minutes."
            maxLength={160}
            showCounter
          />

          <Input
            label="Anything else we should know?"
            value={feedback}
            onChangeText={setFeedback}
            placeholder="Share follow-up details, missing context, or anything we should improve."
            maxLength={500}
            showCounter
          />

          <Text style={styles.helpfulQuestion}>Was the response helpful?</Text>
          <View style={styles.helpfulButtons}>
            <Button
              variant={wasHelpful === true ? 'primary' : 'outline'}
              size="medium"
              onPress={() => setWasHelpful(true)}
              style={styles.helpfulButton}
            >
              Yes
            </Button>
            <Button
              variant={wasHelpful === false ? 'primary' : 'outline'}
              size="medium"
              onPress={() => setWasHelpful(false)}
              style={styles.helpfulButton}
            >
              No
            </Button>
          </View>
        </View>

        <View style={styles.actions}>
          <Button
            variant="outline"
            size="large"
            fullWidth
            icon="description"
            onPress={handleShareReport}
            style={styles.actionButton}
          >
            Download Incident Report
          </Button>

          <Button
            variant="primary"
            size="large"
            fullWidth
            onPress={handleSubmitRating}
            disabled={isClosing}
          >
            {rating > 0 || summaryNote.trim() || feedback.trim() || wasHelpful !== null ? 'Submit & Close' : 'Close Incident'}
          </Button>
        </View>
      </ScrollView>
    </SafeAreaView>
  );
};

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.background,
  },
  content: {
    flexGrow: 1,
    padding: spacing['2xl'],
    paddingBottom: spacing['4xl'],
  },
  centeredState: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    padding: spacing.xl,
    gap: spacing.lg,
  },
  loadingText: {
    fontSize: fontSizes.md,
    color: colors.textSecondary,
    textAlign: 'center',
  },
  header: {
    alignItems: 'center',
    marginTop: spacing.xl,
    marginBottom: spacing['2xl'],
  },
  iconContainer: {
    marginBottom: spacing.lg,
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
    textAlign: 'center',
    lineHeight: 22,
  },
  summaryCard: {
    marginBottom: spacing.lg,
  },
  sectionCard: {
    backgroundColor: colors.surface,
    borderRadius: 28,
    padding: spacing.xl,
    marginBottom: spacing.lg,
    borderWidth: 1,
    borderColor: 'rgba(15, 23, 42, 0.06)',
    shadowColor: '#0F172A',
    shadowOffset: { width: 0, height: 12 },
    shadowOpacity: 0.08,
    shadowRadius: 18,
    elevation: 6,
  },
  cardTitle: {
    fontSize: fontSizes.lg,
    fontWeight: '600',
    color: colors.textPrimary,
    marginBottom: spacing.lg,
  },
  summaryRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: spacing.md,
    borderBottomWidth: 1,
    borderBottomColor: colors.border,
  },
  summaryRowLast: {
    borderBottomWidth: 0,
    paddingBottom: 0,
  },
  summaryLabel: {
    flex: 1,
    fontSize: fontSizes.sm,
    color: colors.textSecondary,
    marginLeft: spacing.md,
  },
  summaryValue: {
    fontSize: fontSizes.md,
    fontWeight: '600',
    color: colors.textPrimary,
  },
  respondersCard: {
    marginBottom: spacing.lg,
  },
  emptyStateText: {
    color: colors.textSecondary,
    lineHeight: 22,
  },
  responderItem: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: spacing.md,
    borderBottomWidth: 1,
    borderBottomColor: colors.border,
  },
  responderInfo: {
    marginLeft: spacing.md,
    flex: 1,
  },
  responderName: {
    fontSize: fontSizes.md,
    fontWeight: '600',
    color: colors.textPrimary,
  },
  responderMeta: {
    marginTop: spacing.xs,
    color: colors.textSecondary,
  },
  ratingCard: {
    marginBottom: spacing.lg,
  },
  feedbackHint: {
    color: colors.textSecondary,
    lineHeight: 20,
    marginBottom: spacing.lg,
  },
  starsContainer: {
    flexDirection: 'row',
    justifyContent: 'center',
    gap: spacing.sm,
    marginBottom: spacing.xl,
  },
  helpfulQuestion: {
    fontSize: fontSizes.md,
    fontWeight: '600',
    color: colors.textPrimary,
    marginBottom: spacing.md,
  },
  helpfulButtons: {
    flexDirection: 'row',
    gap: spacing.md,
  },
  helpfulButton: {
    flex: 1,
  },
  actions: {
    gap: spacing.md,
  },
  actionButton: {
    marginBottom: spacing.sm,
  },
});
