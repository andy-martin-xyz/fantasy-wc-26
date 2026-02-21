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

Requires: Go 1.25+, Firebase CLI, Java (for Firestore emulator)

```bash
# Terminal 1 — Firebase emulators (Auth + Firestore)
firebase emulators:start

# Terminal 2 — Go API server
./dev.sh

# Terminal 3 — Static file server
python3 -m http.server 3000
```

App: http://localhost:3000 · Emulator UI: http://localhost:4000

### Seed test players

```bash
./seed.sh
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
js/                Frontend JS (Alpine.js, Firebase, API client)
css/               Design system (dark mode, mobile-first)
```
