import React from 'react';
import { Text, StyleSheet } from 'react-native';
import { Marker, Callout } from 'react-native-maps';
import { colors } from '@/theme/colors';

interface LocationMarker {
  name: string;
  latitude: number;
  longitude: number;
  severity?: number;
}

interface Props {
  locations: LocationMarker[];
  onLocationPress?: (location: LocationMarker) => void;
}

export const LocationNameOverlay: React.FC<Props> = ({ locations, onLocationPress }) => {
  const getSeverityColor = (severity?: number) => {
    if (!severity) return colors.textSecondary;
    if (severity >= 4) return '#EF4444'; // Red
    if (severity >= 3) return '#F59E0B'; // Amber
    if (severity >= 2) return '#FBBF24'; // Yellow
    return colors.success; // Green
  };

  return (
    <>
      {locations.map((location, index) => (
        <Marker
          key={`location-${index}-${location.latitude}-${location.longitude}`}
          coordinate={{
            latitude: location.latitude,
            longitude: location.longitude,
          }}
          onPress={() => onLocationPress?.(location)}
        >
          <Callout>
            <Text style={[styles.locationName, { color: getSeverityColor(location.severity) }]}>
              {location.name}
            </Text>
          </Callout>
        </Marker>
      ))}
    </>
  );
};

const styles = StyleSheet.create({
  locationName: {
    fontSize: 12,
    fontWeight: '600',
    padding: 4,
  },
});
