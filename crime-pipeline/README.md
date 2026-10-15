# SafeAround Crime Pipeline

Fully automated India-wide crime tracking system.  
Runs every 15 minutes, fetches news, extracts crime details, geocodes locations, and pushes to PostgreSQL.

---

## Architecture

```
crime-pipeline/
├── crime_pipeline.py     ← ORCHESTRATOR  (Phase 5: ETL main loop + FastAPI)
├── news_fetcher.py       ← Phase 1: 9 RSS sources + NewsAPI, parallel fetch, dedup
├── nlp_processor.py      ← Phase 2: 12 crime categories, spaCy NER, severity scoring
├── geocoding_service.py  ← Phase 3: Google Maps API + Redis cache + offline fallback
├── database_service.py   ← Phase 4: Connection pool, batch upsert, NOTIFY, heatmap refresh
├── area_specific_tracker.py ← Hyper-local area-based tracker (runs alongside)
├── config.py             ← 65+ target areas + all env config
├── requirements.txt
├── .env.example
├── main.py               ← FastAPI entry point
├── scheduler.py          ← Standalone scheduler (pipeline only, no API)
└── deploy/
    └── safearound-crime-pipeline.service  ← systemd
```

---

## Pipeline Algorithm (per cycle)

```
[9 News Sources] ──(parallel RSS + NewsAPI)──▶ [NewsFetcherService]
                                                       ↓ 50-200 articles
                                               [NLPProcessorService]
                                          crime type + severity + location text
                                                       ↓
                                          [GeocodingService] ←→ [Google Maps API]
                                          lat/lng (Redis-cached, 30-day TTL)
                                                       ↓
                                          [DatabaseService] → [PostgreSQL + PostGIS]
                                       batch upsert + NOTIFY crime_channel
                                                       ↓
                                         [WebSocket] → [Mobile Apps]
                                          heatmap refresh + real-time alerts
```

---

## Setup

### 1. Install dependencies

```bash
cd crime-pipeline
pip install -r requirements.txt
python -m spacy download en_core_web_lg   # Best accuracy
# OR: python -m spacy download en_core_web_sm  (smaller, faster fallback)
```

### 2. Configure environment

```bash
cp .env.example .env
# Edit .env with your actual values:
#   DB_HOST, DB_NAME, DB_USER, DB_PASSWORD
#   GOOGLE_MAPS_API_KEY  ← get at console.cloud.google.com
#   NEWS_API_KEY         ← get at newsapi.org (optional)
```

### 3. Run

```bash
# Development (standalone, no API server)
python crime_pipeline.py

# Production (with FastAPI health/stats/trigger endpoints)
uvicorn main:app --host 0.0.0.0 --port 8001
```

### 4. Production (systemd auto-start)

```bash
sudo cp deploy/safearound-crime-pipeline.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable safearound-crime-pipeline
sudo systemctl start safearound-crime-pipeline
sudo journalctl -u safearound-crime-pipeline -f   # Live logs
```

---

## API Endpoints

| Method | Path      | Description                              |
|--------|-----------|------------------------------------------|
| GET    | /health   | Status: healthy / degraded / unhealthy   |
| GET    | /stats    | Pipeline + DB statistics                 |
| POST   | /trigger  | Manually trigger one pipeline cycle      |

### Sample `/health` response

```json
{
  "status": "healthy",
  "database_connected": true,
  "last_successful_run": "2026-03-21T13:00:00Z",
  "articles_processed_today": 847,
  "articles_inserted_today": 312,
  "geocoding_success_rate_pct": 94.2,
  "errors_consecutive": 0,
  "avg_cycle_duration_sec": 387.4
}
```

---

## Success Criteria

| Metric                         | Target     |
|--------------------------------|------------|
| Articles fetched per cycle     | 50-200     |
| NLP processing success         | ≥ 70%      |
| Geocoding success rate         | ≥ 90%      |
| New crimes inserted per cycle  | 10-100     |
| Full cycle duration            | < 10 min   |
| Uptime (no manual intervention)| 24+ hours  |
| WebSocket latency (DB → app)   | < 1 second |
