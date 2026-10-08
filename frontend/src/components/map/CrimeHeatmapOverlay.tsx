import React, { useEffect, useState } from 'react';
import MapView, { Heatmap, Circle } from 'react-native-maps';
import { Platform } from 'react-native';
import { heatmapService } from '@/services/api/heatmapService';

interface HeatmapPoint {
  latitude: number;
  longitude: number;
  weight: number;
  location_name?: string;
}

interface Props {
  mapRef: React.RefObject<MapView>;
  bounds: {
    north: number;
    south: number;
    east: number;
    west: number;
  };
  onCrimeDataLoaded?: (count: number) => void;
  onLocationNamesLoaded?: (locations: Array<{ name: string; latitude: number; longitude: number; severity?: number }>) => void;
}

export default function CrimeHeatmapOverlay({ bounds, onCrimeDataLoaded, onLocationNamesLoaded }: Props) {
  const [heatmapPoints, setHeatmapPoints] = useState<HeatmapPoint[]>([]);

  useEffect(() => {
    if (bounds.north !== 0) {
      loadHeatmapData();
    }
  }, [bounds]);

  const loadHeatmapData = async () => {
    try {
      const crimeData = await heatmapService.getCrimeData(bounds);
      
      // Extract unique location names for display
      const locationMap = new Map<string, { name: string; latitude: number; longitude: number; severity: number }>();
      
      // Convert to heatmap points with dynamic time-decay weights
      const points: HeatmapPoint[] = crimeData.map(crime => {
        // AI Time Decay: Use dynamic weight_pct (0-100) if available, else fallback to severity
        const normalizedWeight = crime.weight_pct !== undefined 
          ? Math.max(0.1, crime.weight_pct / 100.0) // Floor at 0.1 so even old crimes show faintly
          : (crime.severity || 1) / 4.0;

        // Track location names with highest severity at each location
        if (crime.location_name) {
          const key = `${crime.latitude.toFixed(2)},${crime.longitude.toFixed(2)}`;
          const existing = locationMap.get(key);
          if (!existing || (crime.severity || 0) > existing.severity) {
            locationMap.set(key, {
              name: crime.location_name,
              latitude: crime.latitude,
              longitude: crime.longitude,
              severity: crime.severity || 1,
            });
          }
        }

        return {
          latitude: crime.latitude,
          longitude: crime.longitude,
          weight: normalizedWeight,
          location_name: crime.location_name,
        };
      });

      setHeatmapPoints(points);
      onCrimeDataLoaded?.(points.length);
      
      // Pass location names to parent for display
      if (onLocationNamesLoaded) {
        onLocationNamesLoaded(Array.from(locationMap.values()));
      }
    } catch (error) {
      console.error('Failed to load heatmap data:', error);
    }
  };

  // For Android/iOS native heatmap (Requires Google Maps Provider)
  // CRITICAL: We only use native Heatmap on Android or if specifically enabled. 
  // iOS Expo Go often lacks the AIRMapHeatmap native config, causing crashes.
  const useNativeHeatmap = Platform.OS === 'android'; 

  if (useNativeHeatmap) {
    if (heatmapPoints.length === 0) return null;
    
    return (
      <Heatmap
        points={heatmapPoints}
        radius={50} // Capped at 50 to prevent "Radius not within bounds"
        opacity={0.7} 
        gradient={{
          colors: [
            'rgba(34, 197, 94, 0.2)',  // Safe Green (Fade in)
            'rgba(34, 197, 94, 0.5)',  // Safe Green (Mid)
            'rgba(234, 179, 8, 0.7)',  // Warning Yellow
            'rgba(239, 68, 68, 0.9)',  // Danger Red
          ],
          startPoints: [0.01, 0.25, 0.6, 1.0], // Wide spread for organic look
          colorMapSize: 2000, // Maximum resolution for smooth blending
        } as any}
      />
    );
  }

  // Helper to interpolate color based on weight (0.1 to 1.0)
  const getInterpolatedColor = (weight: number, alpha: number) => {
    // Green (34, 197, 94) -> Amber (245, 158, 11) -> Red (239, 68, 68)
    if (weight <= 0.4) {
      return `rgba(34, 197, 94, ${alpha})`;
    } else if (weight <= 0.7) {
      return `rgba(245, 158, 11, ${alpha})`;
    }
    return `rgba(239, 68, 68, ${alpha})`;
  };

  // Fallback/Simulated: Multi-layered soft glow for "spreading" effect
  // Flattening to avoid React.Fragment issues in some react-native-maps versions
  return (
    <>
      {heatmapPoints.flatMap((point, idx) => {
        const weight = point.weight || 0.1;
        const baseRadius = 2500 * (weight + 0.3);
        // Use index and timestamp to ensure unique keys even for duplicate coordinates
        const pointKey = `crime-${idx}-${point.latitude.toFixed(6)}-${point.longitude.toFixed(6)}-${Date.now()}`;

        return [
          <Circle
            key={`${pointKey}-outer`}
            center={{ latitude: point.latitude, longitude: point.longitude }}
            radius={baseRadius * 1.5}
            fillColor={getInterpolatedColor(weight, 0.03)}
            strokeColor="transparent"
          />,
          <Circle
            key={`${pointKey}-primary`}
            center={{ latitude: point.latitude, longitude: point.longitude }}
            radius={baseRadius}
            fillColor={getInterpolatedColor(weight, 0.08)}
            strokeColor="transparent"
          />,
          <Circle
            key={`${pointKey}-inner`}
            center={{ latitude: point.latitude, longitude: point.longitude }}
            radius={baseRadius * 0.6}
            fillColor={getInterpolatedColor(weight, 0.15)}
            strokeColor="transparent"
          />,
          <Circle
            key={`${pointKey}-hotspot`}
            center={{ latitude: point.latitude, longitude: point.longitude }}
            radius={baseRadius * 0.25}
            fillColor={getInterpolatedColor(weight, 0.35)}
            strokeColor="transparent"
          />
        ];
      })}
    </>
  );
}
