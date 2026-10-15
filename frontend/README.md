# SafeAround Mobile App

The React Native (Expo) app: crime heatmap, SOS alerts and responding to nearby ones, emergency contacts, safe routes, and danger-zone warnings.

## Requirements

- Node.js 20 or later
- Android Studio (Android) or Xcode (iOS) for local builds; the app uses native modules, so it runs as a development build rather than in Expo Go
- A physical phone for location and push notifications

## Running Locally

```bash
npm install
npm run android   # or: npm run ios  (builds and installs a development build)
npm start         # just the Metro bundler, once a development build is installed
```

Native changes (new native packages or edits to `app.config.js`) need a fresh `npm run android` / `npm run ios`.

## Configuration

- **API and WebSocket URLs, Maps key:** `.env.development` and `.env.production` (`EXPO_PUBLIC_API_URL`, `EXPO_PUBLIC_WS_URL`, `EXPO_PUBLIC_GOOGLE_MAPS_API_KEY`). Without them the app falls back to the production backend (`src/config/env.ts`).
- **App config:** `app.config.js` (name, permissions, plugins, EAS project ID).
- **Firebase (Android push):** `google-services.json` in this folder; it is the file Android builds use.
- **EAS builds:** `eas.json` (`eas build -p android --profile preview` for an installable APK).

## Project Structure

```
frontend/
├── App.tsx / index.ts        # Entry point; startup runs src/utils/initializeApp.ts
├── app.config.js             # Expo config
├── assets/                   # Icons, splash screens, fonts, images
└── src/
    ├── components/           # Reusable UI, grouped by area (common, map, profile, ...)
    ├── config/               # Environment and URLs
    ├── hooks/                # useAuth, useLocation, useWebSocket, ...
    ├── navigation/           # App, auth, main tab and per-tab navigators
    ├── screens/              # auth, onboarding, map, emergency, profile
    ├── services/             # API clients (api/), location, notifications, WebSockets
    ├── store/                # Zustand stores (auth, alerts, location, map, settings, user)
    ├── theme/                # Colors, spacing, typography
    ├── types/                # Shared TypeScript types
    └── utils/                # Validation, phone numbers, startup, helpers
```

## Tech Stack

- **Framework:** React Native 0.81 with Expo SDK 54, TypeScript
- **State:** Zustand
- **Navigation:** React Navigation
- **Maps:** react-native-maps, Google Places search
- **Real-time:** native WebSocket (`services/websocket/CrimeWebSocket.ts`) for alerts and crime updates
- **Storage:** login tokens in SecureStore, everything else in AsyncStorage
- **Notifications:** Expo Notifications (Expo push tokens)
- **Forms:** React Hook Form + Zod
- **Animations:** Reanimated 4

## Checks

```bash
npx tsc --noEmit   # type check (also runs in CI)
```

There is no automated test suite for the app yet.
