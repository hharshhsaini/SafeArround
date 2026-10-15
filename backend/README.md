# SafeAround Backend

The Go API behind the SafeAround app: authentication, SOS alerts and dispatch to nearby users, push/SMS notifications, the crime heatmap, location, and safe routes.

## Tech Stack

- **Language:** Go 1.25
- **Framework:** Gin
- **Database:** PostgreSQL with PostGIS
- **Cache / rate limits / token revocation:** Redis
- **External services:** Twilio (Verify OTPs and SMS), Expo push and FCM HTTP v1 (push), Google Maps (geocoding)

## Authentication

1. **Phone OTP:** the user gets a 6-digit code through Twilio Verify; verifying it returns an access token (15 minutes) and a refresh token (7 days, stored as a session).
2. **Password:** after signing in with an OTP, a user can set a password and sign in with phone + password from then on.
3. **Refresh / logout:** `/auth/refresh` rotates the refresh token; `/auth/logout` revokes the session and the current access token.
4. **Forgot password:** `/auth/password/reset` takes the phone, an OTP and the new password, then signs the account out everywhere.

## Running Locally

```bash
make setup   # copies .env.example to .env, downloads modules, starts Postgres + Redis in Docker
make run     # or `make dev` for hot reload with Air
```

The API listens on `http://localhost:8000` (`SERVER_PORT`). See [.env.example](.env.example) for every setting the code reads.

## Database

- **App tables** come from the GORM models in `internal/models`, created by `database.RunMigrations` (`internal/database/migrations.go`). That call is currently commented out in `cmd/api/main.go`.
- **Crime tables, the heatmap view and the live-update trigger** are applied by hand from [`database/triggers/realtime_crime_update.sql`](database/triggers/realtime_crime_update.sql).
- **`migrations/`** holds golang-migrate files (`make migrate-up`). They are older than the models and no longer match them (for example, they create `users.password_hash` while the code uses `users.password`), so don't run them on a fresh database until they're brought up to date.

## Project Structure

```
backend/
├── cmd/
│   ├── api/                 # The API server (main entry point)
│   └── worker/              # Background jobs (separate binary, Dockerfile.worker)
├── internal/
│   ├── config/              # Loads .env settings
│   ├── database/            # Postgres, Redis and GORM schema setup
│   ├── handlers/            # HTTP handlers (Gin)
│   ├── middleware/          # Auth, CORS, rate limits, metrics, security headers
│   ├── models/              # GORM models
│   ├── monitoring/          # Prometheus metrics
│   ├── repository/          # User and session data access
│   ├── routes/              # Route table
│   ├── services/            # Business logic (alerts, notifications, location, routes...)
│   ├── utils/               # JWT, passwords, phone numbers
│   └── websocket/           # Real-time hub for /ws/crime
├── pkg/                     # Clients for FCM/Expo push, Twilio, Google Maps; the logger
├── database/triggers/       # Hand-applied SQL (crime tables, heatmap view, trigger)
├── migrations/              # golang-migrate files (see Database above)
├── scripts/                 # Developer tools, run from backend/:
│   ├── migrate.sh           #   ./scripts/migrate.sh up|down|version|refresh-mv|status
│   ├── check_user/          #   go run ./scripts/check_user
│   ├── create_test_user/    #   go run ./scripts/create_test_user
│   ├── test_login/          #   go run ./scripts/test_login <phone> <password>
│   ├── test_notification/   #   go run ./scripts/test_notification <push_token>
│   └── send_test_otp/       #   go run ./scripts/send_test_otp <phone>
└── tests/                   # API-level tests (unit tests live next to their packages)
```

## Tests and Checks

```bash
make test       # go test ./...
make vet        # go vet ./...
make coverage   # tests with a per-function coverage report
```

The logout token-revocation test needs Redis; it is skipped unless `TEST_REDIS_ADDR` is set (e.g. `TEST_REDIS_ADDR=localhost:6379 make test`). CI runs all of this on every push and pull request.

## License

MIT, see [LICENSE](LICENSE).
