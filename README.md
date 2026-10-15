# SafeAround

SafeAround is a community-driven personal safety mobile application designed to help users make safer movement decisions and receive immediate assistance during emergencies.

The app focuses on safety-aware routing, area safety insights, and fast SOS broadcasting to nearby users and trusted contacts.

---

## Key Features

### SOS Emergency System
- One SOS reaches:
  - Your emergency contacts (push notification if they use SafeAround, and always an SMS with your location)
  - SafeAround users within 100m, widening the search every 30 seconds until someone responds
- Responders get directions to you; you see who is coming
- SafeAround does not call the police for you: call 112 directly

### Area Safety Insights
- Crime heatmap built from verified news reports, refreshed every 15 minutes
- Users can report incidents (stored as unverified, so they don't trigger alerts for others)
- Warnings when entering high-risk areas

### Safe Routing
- Routes that avoid high-crime areas

---

## Repository Layout

| Folder | What it is | Start here |
|---|---|---|
| [`frontend/`](frontend/) | React Native (Expo) mobile app | [frontend/README.md](frontend/README.md) |
| [`backend/`](backend/) | Go API: auth, SOS dispatch, notifications, heatmap, WebSocket | [backend/README.md](backend/README.md) |
| [`crime-pipeline/`](crime-pipeline/) | Python service that turns crime news into heatmap data | [crime-pipeline/README.md](crime-pipeline/README.md) |
| [`deploy/`](deploy/) | Server setup: nginx config, Grafana dashboard, setup and backup scripts | — |
| [`.github/workflows/`](.github/workflows/) | CI: backend tests, app type check, then build and deploy from `main` | — |

How the parts connect:

```
Mobile app ──HTTPS──▶ backend (Go) ──▶ PostgreSQL + PostGIS, Redis
     ▲                    │
     └──── WebSocket ─────┘   (SOS events, crime updates)

crime-pipeline (Python) ──writes──▶ PostgreSQL ──NOTIFY──▶ backend ──▶ app
```

## Running Everything Locally

1. **Backend:** `cd backend && make setup && make run` (API on port 8000)
2. **App:** `cd frontend && npm install && npm run android` (point `.env.development` at your backend)
3. **Crime pipeline (optional):** see [crime-pipeline/README.md](crime-pipeline/README.md)

## Tech Stack

**Backend:** Go, Gin, PostgreSQL + PostGIS, Redis, Twilio, Expo push / FCM

**Mobile app:** React Native with Expo (Android & iOS), TypeScript, Zustand

**Crime pipeline:** Python, FastAPI, spaCy, Google Maps geocoding
