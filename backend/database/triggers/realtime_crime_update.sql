-- backend/database/triggers/realtime_crime_update.sql

-- Ensure postgis is enabled
CREATE EXTENSION IF NOT EXISTS postgis;

-- 1. Create/Update crime_incidents table
CREATE TABLE IF NOT EXISTS crime_incidents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    crime_type VARCHAR(100) NOT NULL,
    severity INT CHECK (severity BETWEEN 1 AND 4),
    location GEOGRAPHY(POINT, 4326) NOT NULL,
    address TEXT,
    description TEXT,
    source VARCHAR(100),
    occurred_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    verified BOOLEAN DEFAULT true
);

-- Spatial index for fast location queries
CREATE INDEX IF NOT EXISTS idx_crime_location ON crime_incidents USING GIST(location);
CREATE INDEX IF NOT EXISTS idx_crime_occurred ON crime_incidents(occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_crime_type_severity ON crime_incidents(crime_type, severity);

-- 2. Heatmap grid materialized view (for fast tile generation)
DROP MATERIALIZED VIEW IF EXISTS mv_crime_heatmap_grid;
CREATE MATERIALIZED VIEW mv_crime_heatmap_grid AS
SELECT
    FLOOR(ST_X(location::geometry) / 0.001)::INT as grid_x,
    FLOOR(ST_Y(location::geometry) / 0.001)::INT as grid_y,
    COUNT(*)::INT as crime_count,
    SUM(severity)::INT as severity_sum,
    AVG(severity)::FLOAT as avg_severity,
    ARRAY_AGG(crime_type) as crime_types,
    MAX(occurred_at) as last_incident
FROM crime_incidents
WHERE occurred_at > NOW() - INTERVAL '30 days'
GROUP BY grid_x, grid_y;

CREATE UNIQUE INDEX ON mv_crime_heatmap_grid(grid_x, grid_y);

-- 3. Real-time trigger for WebSocket notifications
-- Only verified incidents are announced; unverified user reports stay off
-- everyone's screens. The API also drops any new_crime event without
-- "verified": true, so this section can be re-run on its own.
CREATE OR REPLACE FUNCTION notify_crime_update()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify(
        'crime_channel',
        json_build_object(
            'event', 'new_crime',
            'id', NEW.id,
            'crime_type', NEW.crime_type,
            'severity', NEW.severity,
            'lat', ST_Y(NEW.location::geometry),
            'lng', ST_X(NEW.location::geometry),
            'occurred_at', NEW.occurred_at,
            'verified', NEW.verified
        )::text
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS crime_insert_trigger ON crime_incidents;
CREATE TRIGGER crime_insert_trigger
    AFTER INSERT ON crime_incidents
    FOR EACH ROW
    WHEN (NEW.verified IS TRUE)
    EXECUTE FUNCTION notify_crime_update();
