# WC Fantasy 2026

A real-time fantasy soccer draft app for World Cup 2026, built for a small friend group.

## Stack

- **Frontend** — HTML/CSS/Alpine.js, served via Firebase Hosting
- **Backend** — Go (chi router), deployed on Cloud Run
- **Database** — Firestore (real-time listeners for live draft)
- **Auth** — Firebase Auth (Google Sign-In)

## Features

- Live snake draft room with real-time pick updates via Firestore `onSnapshot`
- Countdown timer per pick, turn banner, draft history
- AI pick suggestions powered by Claude (Anthropic API)
- Admin panel — start/pause/resume draft, import players, process match scores
- Leaderboard and team roster views

## Local Development

### Docker (recommended)

Requires: Docker Desktop

```bash
cp .env.example .env
# Fill in your Firebase values (Firebase Console → Project Settings → Your Apps)
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
# Terminal 1 — Firebase emulators (Auth + Firestore)
firebase emulators:start

# Terminal 2 — Go API server
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
internal/
  handlers/        HTTP route handlers
  middleware/       Auth + admin middleware
  models/          Firestore data types + scoring logic
  db/              Firestore client wrapper
public/            Firebase Hosting root (HTML pages, CSS, JS)
scripts/           Dev and seed shell scripts
docs/              Project docs and planning notes
```
