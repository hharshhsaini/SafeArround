import { apiClient } from './client';
import { AreaStats } from '@/types/models';

interface CrimePoint {
  latitude: number;
  longitude: number;
  severity: number;
  crime_type: string;
  weight_pct?: number;
  location_name?: string;
}

const AREA_STATS_CACHE_TTL_MS = 20_000;

type AreaStatsCacheEntry = {
  data: AreaStats;
  expiresAt: number;
};

const areaStatsCache = new Map<string, AreaStatsCacheEntry>();

const areaStatsCacheKey = (lat: number, lng: number) => `${lat.toFixed(3)}:${lng.toFixed(3)}`;

export const heatmapService = {
  // Get crime data points for map bounds
  async getCrimeData(bounds: {
    north: number;
    south: number;
    east: number;
    west: number;
  }): Promise<CrimePoint[]> {
    const response = await apiClient.get('/heatmap/data', {
      params: bounds,
    });
    return response.data?.data || [];
  },

  // Get grid data for smooth heatmap
  async getGridData(bounds: {
    north: number;
    south: number;
    east: number;
    west: number;
  }) {
    const response = await apiClient.get('/heatmap/grid', {
      params: bounds,
    });
    return response.data.data;
  },

  async getTile(z: number, x: number, y: number): Promise<string | null> {
    const response = await apiClient.get(`/heatmap/tiles/${z}/${x}/${y}`, {
      validateStatus: (status) => status >= 200 && status < 500,
    });

    if (response.status !== 200) {
      return null;
    }

    return response.request?.responseURL ?? null;
  },

  // Get statistics for a point
  async getStatistics(lat: number, lng: number): Promise<AreaStats> {
    const cacheKey = areaStatsCacheKey(lat, lng);
    const cached = areaStatsCache.get(cacheKey);
    if (cached && cached.expiresAt > Date.now()) {
      return cached.data;
    }

    const response = await apiClient.get('/heatmap/statistics', {
      params: { lat, lng },
    });
    const stats = {
      safetyScore: Number(response.data?.safetyScore ?? 0),
      nearbyUsers: Number(response.data?.nearbyUsers ?? 0),
      recentAlerts: Number(response.data?.recentAlerts ?? 0),
      crimeRate: Number(response.data?.crimeRate ?? 0),
      lastUpdated: response.data?.lastUpdated ?? new Date().toISOString(),
    };

    areaStatsCache.set(cacheKey, {
      data: stats,
      expiresAt: Date.now() + AREA_STATS_CACHE_TTL_MS,
    });

    return stats;
  },
};
