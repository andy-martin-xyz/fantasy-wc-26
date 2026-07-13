# World Cup Fantasy 2026

A real-time fantasy soccer draft app for World Cup 2026, built for a small friend group.

## Stack

- **Frontend** — HTML/CSS/Alpine.js, served via Firebase Hosting
- **Backend** — Go (chi router), deployed on Cloud Run
- **Database** — Firestore (real-time listeners for live draft)
- **Auth** — Firebase Auth (Google Sign-In)

## Features

- Live snake draft room with real-time pick updates via Firestore `onSnapshot`
- Countdown timer per pick, turn banner, draft history
- Admin panel — start/pause/resume draft, import players, process match scores
- Leaderboard and team roster views

## Local Development

**Important:** only Firestore is emulated. Auth is not — sign-in always hits
your real Firebase project (Google Sign-In), even locally, so the backend
needs a real service account key regardless of which path below you use.

1. In Firebase Console → Project Settings → Service Accounts → Generate new
   private key. Save the download as `keys/service-account.json`
   (gitignored — never commit it).
2. `cp .env.example .env` and fill in your Firebase values, including
   `GOOGLE_APPLICATION_CREDENTIALS` (defaults to the path from step 1).

### Docker (recommended)

Requires: Docker Desktop

```bash
docker compose up
```

App: http://localhost:3000 · API: http://localhost:8081 · Emulator UI: http://localhost:4000

### Manual (three terminals)

Requires: Go 1.25+, Firebase CLI, Java (for Firestore emulator)

```bash
cp public/js/config.js.example public/js/config.js
# Fill in your Firebase values
```

```bash
# Terminal 1 — Firestore emulator (firebase.json also starts an Auth
# emulator, but nothing in this app actually talks to it — ignore it)
firebase emulators:start

# Terminal 2 — Go API server (reads .env from step 2 above)
./scripts/dev.sh

# Terminal 3 — Static file server
python3 -m http.server 3000 --directory public
```

App: http://localhost:3000 · Emulator UI: http://localhost:4000

### Seed test players

```bash
# Docker
docker compose exec api go run ./cmd/seed

# Manual
./scripts/seed.sh
```

## Testing

Requires: Firebase CLI, Java (for the Firestore emulator) — same as manual local dev.

```bash
# Terminal 1 — start the emulator
firebase emulators:start --only firestore

# Terminal 2 — run the suite against it
FIRESTORE_EMULATOR_HOST=localhost:8080 go test ./...
```

Tests that touch Firestore (draft, scoring, team handlers) skip cleanly if
`FIRESTORE_EMULATOR_HOST` isn't set, so plain `go test ./...` never fails on a
machine without the emulator running — it just gets less coverage. CI
(`.github/workflows/ci.yml`) runs the full emulator-backed suite on every PR.

## Deployment

```bash
# Backend (Cloud Run)
gcloud run deploy fantasy-league-api --source . --region us-central1 --project andy-personal-1bb38

# Frontend + Firestore rules (Firebase Hosting)
firebase deploy --only hosting,firestore:rules
```

## Project Structure

```
cmd/server/        Go API entry point
cmd/seed/          Player seed script (local + production)
tools/             One-time per-tournament bootstrap tools (not deployed):
  import/          Builds the player pool from squad TSVs + ratings CSV
  espn-ids/         Enriches tools/import/players.json with ESPN athlete ids
internal/
  handlers/        HTTP route handlers
  middleware/       Auth + admin middleware
  models/          Firestore data types + scoring logic
  db/              Firestore client wrapper
public/            Firebase Hosting root (HTML pages, CSS, JS)
scripts/           Dev and seed shell scripts
docs/              Project docs and planning notes
```
