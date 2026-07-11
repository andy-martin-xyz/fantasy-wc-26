# WC Fantasy 2026 — Progress Tracker

> Last updated: 2026-02-21
> For full architecture and data model, see: `fantasy-wc-plan.md`

---

## Project Summary

Mobile-first fantasy soccer web app for a small friend group.
**Stack:** HTML/CSS/Alpine.js → Firebase Hosting | Go API → Cloud Run | Firestore | Firebase Auth (Google)

```
Browser → Firebase Hosting (static HTML/CSS/Alpine.js)
               ↓
         Firebase Auth (Google Sign-In)
               ↓
         Cloud Run (Go API) ← verifies Firebase ID tokens
               ↓
           Firestore
               ↑
         Firestore onSnapshot listeners (draft page only)
```

---

## File Structure (current)

```
fantasy-league/
├── css/
│   └── styles.css          ✅ complete — full design system, dark mode, mobile-first
├── js/
│   └── app.js              ✅ complete — Alpine.js auth store + all Phase 1 mock data
├── index.html              ✅ complete — leaderboard page
├── team.html               ✅ complete — team view (parses ?id= from URL)
├── my-team.html            ✅ complete — authenticated user's roster
├── draft.html              ✅ complete — draft room UI (static mockup)
├── login.html              ✅ complete — Google sign-in placeholder
├── admin.html              ✅ complete — admin panel with password gate
├── PROGRESS.md             ✅ this file
├── go.mod                  ✅ complete
├── go.sum                  ✅ complete
├── Dockerfile              ✅ complete
├── cmd/
│   └── server/
│       └── main.go         ✅ complete — chi router, CORS, all routes wired
└── internal/
    ├── models/
    │   └── models.go       ✅ complete — all Firestore types + scoring logic
    ├── db/
    │   └── firestore.go    ✅ complete — Firestore client wrapper, emulator support
    ├── middleware/
    │   └── auth.go         ✅ complete — Authenticate + RequireAdmin
    └── handlers/
        ├── handlers.go     ✅ complete — shared Handler struct + response helpers
        ├── user.go         ✅ complete — register, update team name, GET /api/users
        ├── players.go      ✅ complete — import players, get players
        ├── leaderboard.go  ✅ complete — leaderboard, team view
        ├── draft.go        ✅ complete — set-order, start/pause/resume, pick (transactional)
        ├── suggest.go      ✅ complete — AI pick suggestions (Anthropic + local fallback)
        └── scores.go       ✅ complete — upsert match, process scores
```

---

## Phase 1 — Static Frontend ✅ COMPLETE

Goal: Mobile UI with hardcoded mock data. Pure HTML/CSS/Alpine.js, no backend.

| Task | Description | Status |
|------|-------------|--------|
| 1.1 | Project scaffolding — directory structure, shared CSS, `js/app.js`, Alpine.js CDN on all pages, shared nav | ✅ Done |
| 1.2 | Leaderboard (`index.html`) — 8 mock users, ranked cards, #1 gold, tap → team view | ✅ Done |
| 1.3 | Team view (`team.html`) — parse `?id=` from URL, roster grouped by GK/DEF/MID/FWD, player points | ✅ Done |
| 1.4 | Draft room (`draft.html`) — round/pick header, countdown timer (static), turn banner, position tabs, search, player list, draft history | ✅ Done |
| 1.5 | Login + auth placeholder (`login.html`, `my-team.html`) — Google sign-in button (non-functional), Alpine.js auth store, nav shows correct state | ✅ Done |
| 1.6 | Admin panel (`admin.html`) — password gate ("wc2026"), draft controls, match results form, player import, draft order (randomizable) | ✅ Done |

**Auth toggle for testing Phase 1:**
In `js/app.js`, set `isLoggedIn: false` or `isAdmin: false` in `Alpine.store('auth', {...})` to test different nav states.

---

## Phase 2 — Go API + Firestore ✅ COMPLETE

Goal: Full backend. All endpoints working, tested locally against Firestore emulator.

**Prerequisites:** Install Go 1.22+, Docker, Firebase CLI, set up Firestore emulator.

| Task | Description | Status |
|------|-------------|--------|
| 2.1 | Go project scaffolding — `go mod init`, `cmd/server/main.go`, `internal/` directories, `chi` router, health check `GET /api/health`, CORS middleware, `Dockerfile` | ✅ Done |
| 2.2 | Firestore client — `internal/db/firestore.go`, Firebase Admin SDK, supports `FIRESTORE_EMULATOR_HOST` env var, `GetDoc`/`SetDoc`/`UpdateDoc` helpers | ✅ Done |
| 2.3 | Auth middleware — `internal/middleware/auth.go`, verify Firebase ID token from `Authorization: Bearer <token>`, attach UID/email to context, `RequireAdmin` middleware checks `isAdmin` flag in Firestore | ✅ Done |
| 2.4 | User endpoints — `POST /api/user/register` (idempotent, pulls name/email/photo from token, creates empty roster doc), `PUT /api/user/team-name` (validates non-empty, not taken) | ✅ Done |
| 2.5 | Player import — `POST /api/admin/players/import`, accepts JSON array, validates position (GK/DEF/MID/FWD), upserts by name+country, preserves draft state on re-import | ✅ Done |
| 2.6 | Leaderboard + team endpoints — `GET /api/leaderboard`, `GET /api/team/{uid}` with player point breakdowns, both public | ✅ Done |
| 2.7 | Draft endpoints — set-order (explicit or randomize), start, pause, resume, status, `POST /api/draft/pick` (Firestore transaction: validates turn/availability/position limits, snake order) | ✅ Done |
| 2.8 | Scoring — `PUT /api/admin/match/{matchId}`, `POST /api/admin/scores/process` (writes stats, recalcs player→roster→leaderboard, idempotent) | ✅ Done |

**Scoring rules (implement exactly):**
| Event | Points |
|-------|--------|
| Goal | +5 |
| Assist | +3 |
| Clean sheet (GK/DEF only) | +4 |
| Yellow card | -1 |
| Red card | -3 |
| Team win | +2 |
| Team draw | +1 |

**Roster limits (enforce in `POST /api/draft/pick`):**
1 GK, 4 DEF, 4 MID, 2 FWD = 11 players total

**Snake draft order logic:**
- Round 1: u1→u2→…→uN
- Round 2: uN→…→u2→u1
- Alternates each round
- `currentPickIndex` in `draft/config` is the global index into the full flattened pick sequence

---

## Phase 3 — Connect Frontend to Backend + Auth ✅ COMPLETE

Goal: Replace all hardcoded mock data with live API calls. Wire Firebase Auth.

| Task | Description | Status |
|------|-------------|--------|
| 3.1 | Firebase setup guide (`FIREBASE_SETUP.md`) — create project, enable Firestore/Auth/Hosting, security rules, emulator setup, `firebase.json` with `/api/**` rewrite to Cloud Run | ✅ Done |
| 3.2 | `js/auth.js` — Firebase compat CDN, Google `signInWithPopup`, `onAuthStateChanged` updates `Alpine.store('auth')`, auto-registers new users, redirects protected pages, no FOUC spinner | ✅ Done |
| 3.3 | `js/api.js` — auto-attaches ID token as Bearer, 401→login redirect, dispatches `api-error` event, exports `window.Api.*` for all endpoints | ✅ Done |
| 3.4 | `index.html` — live `Api.getLeaderboard()`, skeleton loading state, empty state, auto-refresh every 60s | ✅ Done |
| 3.5 | `team.html` — live `Api.getTeam(uid)`, skeleton loading, not-found state | ✅ Done |
| 3.6 | `admin.html` — Firebase auth gate (isAdmin check), all controls wired to API (draft start/pause/resume, match save, process scores, import players), `draft.html`/`my-team.html`/`login.html` all wired | ✅ Done |

**New files:**
- `js/config.js` — Firebase config + `API_BASE=http://localhost:8081` for local dev
- `js/auth.js` — Firebase Auth integration
- `js/api.js` — API client (`window.Api.*`)
- `firebase.json` — Hosting + Firestore emulator config (port 8080), Auth emulator (port 9099)
- `FIREBASE_SETUP.md` — Step-by-step Firebase setup guide
- `dev.sh` — Local dev launcher script (sets all env vars, runs Go server on port 8081)
- `DEBUGGING.md` — Full debugging notes from Phase 3 bring-up

**Bug fixes applied during Phase 3 bring-up (see DEBUGGING.md for full details):**
- `db/firestore.go` — When `FIRESTORE_EMULATOR_HOST` is set, create Firestore client directly (no Admin SDK credentials) so gRPC connects plaintext to emulator
- `middleware/auth.go` — Store `name`/`email`/`picture` JWT claims in context; added `EmailFromContext`, `DisplayNameFromContext`, `PhotoURLFromContext`
- `handlers/user.go` — Removed `h.Auth.GetUser()` call; use JWT claims from context instead (eliminates blocking Firebase Auth API call)
- `cmd/server/main.go` — CORS middleware: allow any request with an Origin header for local dev; added debug logging; Go server runs on port 8081 (Firestore emulator is on 8080)
- `firebase.json` — Added `firestore` emulator on port 8080 (Java required: `brew install openjdk`)

**Firebase project:** `andy-personal-1bb38`

**To run locally:**
```bash
# Terminal 1
firebase emulators:start
# Terminal 2
./dev.sh
# Terminal 3
python3 -m http.server 3000
```

---

## Phase 4 — Live Draft ✅ COMPLETE

Goal: Real-time draft, fully functional.

| Task | Description | Status |
|------|-------------|--------|
| 4.1 | Firestore real-time listeners — `draft.html` uses `onSnapshot` on `draft/config` and `draft/picks/items` sub-collection, auto-updates whose turn it is, round, timer, adds picks to history, removes drafted players | ✅ Done |
| 4.2 | Pick submission — "Draft" button calls `POST /api/draft/pick`, loading state per player, error handling (not your turn, player taken), buttons only enabled on user's turn | ✅ Done |
| 4.3 | Countdown timer — real `setInterval` timer reading `currentPickDeadline` Firestore timestamp, green >30s, yellow 10-30s, red <10s, resets automatically when next pick starts | ✅ Done |
| 4.4 | Draft history + roster builder — picks log auto-scrolls to latest pick, roster slots built from Firestore pick history (no extra API call needed) | ✅ Done |
| 4.5 | Player search + filtering — position tabs (ALL/GK/DEF/MID/FWD), text search by name or country, drafted players removed in real-time via Firestore listener, all players loaded from API | ✅ Done |
| 4.6 | Client-side suggestion engine (`localSuggest` in suggest.go) — analyzes current roster, picks top 3 by position need then rating. Fallback when no `ANTHROPIC_API_KEY` set. | ✅ Done |
| 4.7 | Claude-powered draft advisor — `POST /api/draft/suggest` Go endpoint calls Anthropic API (`claude-sonnet-4-6`), builds roster+available context, returns top 3 suggestions with explanations. Seamlessly falls back to client-side engine on error or missing key. | ✅ Done |

**New files (Phase 4):**
- `internal/handlers/suggest.go` — `SuggestPick` handler + `callAnthropic` + `localSuggest` fallback

**Modified files (Phase 4):**
- `draft.html` — full rewrite: Firestore listeners, real timer, users map, AI suggestions panel, live history
- `internal/handlers/user.go` — added `GET /api/users` (public, returns display names)
- `cmd/server/main.go` — added `GET /api/users` and `POST /api/draft/suggest` routes
- `js/api.js` — added `Api.getUsers()` and `Api.getDraftSuggestions()`
- `dev.sh` — added `ANTHROPIC_API_KEY` comment placeholder

**Firestore listener paths:**
- `draft/config` → status, currentPickIndex, pickOrder, currentPickDeadline, round
- `draft/picks/items` (orderBy pickNumber) → pick history, drafted player IDs

**Snake draft logic is mirrored in JS** (`computePickerUID`) matching Go's `DraftConfig.CurrentPickerUID()`.

**To enable AI suggestions:** uncomment and set `ANTHROPIC_API_KEY=sk-ant-...` in `dev.sh`.

---

## Phase 5 — Deployment & Polish ✅ COMPLETE

Goal: Deploy, test end-to-end, polish.

| Task | Description | Status |
|------|-------------|--------|
| 5.1 | Cloud Run deployment — source deploy via `gcloud run deploy --source .`, billing enabled on project, `/api/health` verified | ✅ Done |
| 5.2 | Firebase Hosting deployment — `firebase.json` with `/api/**` Cloud Run rewrite, `firestore.rules` for authenticated reads, `firebase deploy --only hosting,firestore:rules` | ✅ Done |
| 5.3 | Player data seeding — 35 test players seeded to production via `./seed-prod.sh` (uses service account + `SEED_PRODUCTION=true` guard) | ✅ Done |
| 5.4 | Scoring automation (stretch) — manual "Process Scores" admin flow; Cloud Scheduler deferred | ⏭ Skipped |
| 5.5 | Mobile polish — design is already mobile-first; PWA manifest deferred | ⏭ Skipped |
| 5.6 | Pre-draft-night testing — live at https://andy-personal-1bb38.web.app, testing with friend in progress | 🔄 In progress |

**Live URLs:**
- App: https://andy-personal-1bb38.web.app
- API: https://fantasy-league-api-439489396970.us-central1.run.app

**Production env vars (Cloud Run):**
- `FIREBASE_PROJECT_ID=andy-personal-1bb38`
- `ALLOWED_ORIGIN=https://andy-personal-1bb38.web.app`
- (no `FIRESTORE_EMULATOR_HOST` — connects to real Firestore via ADC)

**To redeploy after changes:**
```bash
# API (Go server):
gcloud run deploy fantasy-league-api --source . --region us-central1 --project andy-personal-1bb38

# Frontend (static files + rules):
firebase deploy --only hosting,firestore:rules
```

---

## Phase 6 — Automated Scoring ❌ NOT STARTED

Goal: Automatically pull real match stats from a third-party API after each World Cup game and update player points + leaderboard without manual admin intervention.

**Recommended stats API:** [API-Football](https://www.api-football.com) (RapidAPI) — has WC 2026 coverage, free tier (100 req/day), player-level stats per fixture.

**Core approach:** Pull official WC 2026 squads from API-Football once they're announced (~June 2026), use those to populate the player list with `apiFootballId` stored on each doc. Scoring then looks up by ID directly — no name matching needed.

| Task | Description | Status |
|------|-------------|--------|
| 6.1 | Squad import — `POST /api/admin/squads/fetch` calls API-Football `/players/squads?team={id}` for all 32 WC teams, writes players to Firestore with `apiFootballId` field. Replaces the manual seed script for real tournament. | ❌ Todo |
| 6.2 | Stats API integration — Go client for API-Football (`GET /fixtures/players?fixture={id}`), map response to `PlayerMatchStats` using `apiFootballId` for exact player lookup | ❌ Todo |
| 6.3 | Score fetch endpoint — `POST /api/admin/scores/fetch/{matchId}` pulls stats from API-Football, matches players by `apiFootballId`, calls existing `ProcessScores` logic. Admin triggers this after each game. | ❌ Todo |
| 6.4 | Match fixture mapping — store API-Football fixture ID on match docs so `scores/fetch` knows which fixture to pull. Admin sets this when creating a match in the admin panel. | ❌ Todo |
| 6.5 | Admin review UI — show fetched stats in `admin.html` before committing, so admin can spot any players not found before points are applied | ❌ Todo |
| 6.6 | Cloud Scheduler (stretch) — automate score fetching ~90 min after each kickoff rather than manual admin trigger | ❌ Todo |

**New env var needed:**
```
API_FOOTBALL_KEY=your-rapidapi-key   # add to Cloud Run and dev.sh
```

**Suggested implementation order:** 6.1 → 6.4 → 6.2 → 6.3 → 6.5 → 6.6

---

## Proposed Test Cases

### Phase 1 — Manual browser tests (open HTML files directly)

| # | Test | Expected |
|---|------|----------|
| P1-1 | Open `index.html` | 8 ranked cards visible, #1 card has gold border/glow |
| P1-2 | Tap a leaderboard card | Navigates to `team.html?id=<uid>` |
| P1-3 | Open `team.html?id=u1` | Shows Andrew's roster grouped GK→DEF→MID→FWD with point values |
| P1-4 | Open `team.html?id=u2` | Shows Jake's roster (different players) |
| P1-5 | Open `team.html?id=u99` (bad id) | Shows "Team not found" empty state |
| P1-6 | Open `draft.html` | Timer shows red (< 10s), Mike's turn banner shown, 18 players listed |
| P1-7 | Type in draft search box | Player list filters in real time |
| P1-8 | Click position tabs in draft | List filters to correct positions only |
| P1-9 | Open `login.html` | Centered card with Google sign-in button, no nav auth links |
| P1-10 | Open `admin.html`, enter wrong password | Shows "Incorrect password" error |
| P1-11 | Open `admin.html`, enter `wc2026` | Unlocks admin panel |
| P1-12 | Click "Randomize Order" in admin | Draft order shuffles visually |
| P1-13 | Click "Load Example" in player import | JSON populates textarea |
| P1-14 | Paste invalid JSON, click Import | Shows "Invalid JSON" toast |
| P1-15 | Set `isLoggedIn: false` in `app.js` | Nav shows "Sign In" link, not avatar/My Team/Draft links |
| P1-16 | Open on 375px wide viewport (iPhone) | No horizontal scroll, all tap targets comfortable |
| P1-17 | Open on desktop (> 640px) | Content centered, side borders visible, max-width respected |

### Phase 2 — Go API unit + integration tests

| # | Test | Expected |
|---|------|----------|
| P2-1 | `GET /api/health` | 200 `{"status":"ok"}` |
| P2-2 | `POST /api/user/register` with valid Firebase token | 200, user doc created in Firestore |
| P2-3 | `POST /api/user/register` twice with same token | 200 both times, no duplicate doc |
| P2-4 | `PUT /api/user/team-name` with empty name | 400 error |
| P2-5 | `PUT /api/user/team-name` with name already taken | 409 conflict |
| P2-6 | `POST /api/admin/players/import` without admin token | 403 |
| P2-7 | `POST /api/admin/players/import` with valid JSON | 200, players written to Firestore |
| P2-8 | `POST /api/admin/players/import` same players again | 200, upserted (no duplicates) |
| P2-9 | `GET /api/leaderboard` (no auth) | 200, returns sorted standings array |
| P2-10 | `GET /api/team/{uid}` (no auth) | 200, returns roster with points |
| P2-11 | `GET /api/team/nonexistent` | 404 |
| P2-12 | `POST /api/draft/pick` when not your turn | 400 "not your turn" |
| P2-13 | `POST /api/draft/pick` with already-drafted player | 400 "player already drafted" |
| P2-14 | `POST /api/draft/pick` valid pick | 200, pick doc created, player marked drafted, roster updated, pick index advanced |
| P2-15 | Draft pick at end of round 1 (last user) | Next picker is last user again (snake reversal) |
| P2-16 | Draft pick exceeding position limit (e.g. 5th DEF) | 400 "position limit reached" |
| P2-17 | `POST /api/admin/scores/process` for a match | Player/roster totalPoints updated, leaderboard rebuilt |
| P2-18 | Reprocess same match | Previous scores replaced, totals correct (idempotent) |
| P2-19 | Unauthenticated request to protected route | 401 |
| P2-20 | Non-admin token on admin route | 403 |

### Phase 3 — Frontend integration tests

| # | Test | Expected |
|---|------|----------|
| P3-1 | Load `index.html` with real backend | Leaderboard fetched from API, not mock data |
| P3-2 | Click Google sign-in | Firebase popup opens, user signed in |
| P3-3 | First sign-in | `POST /api/user/register` called automatically |
| P3-4 | Sign out | Auth store cleared, nav shows Sign In |
| P3-5 | Load `team.html?id=<real-uid>` | Fetches from `GET /api/team/{uid}` |
| P3-6 | Load leaderboard page | Shows loading skeleton, then data |
| P3-7 | API returns 401 | Auto-redirected to `login.html` |
| P3-8 | Admin panel submit match | Calls API, shows success toast |

### Phase 4 — Draft real-time tests

| # | Test | Expected |
|---|------|----------|
| P4-1 | Two browsers open `draft.html` | Both show same current picker and round |
| P4-2 | Admin advances pick (via Firestore emulator or admin panel) | Both browsers update without refresh |
| P4-3 | Current user makes a valid pick | Player disappears from available list on all browsers |
| P4-4 | Timer reaches 0 | "Time's up!" shown, pick buttons disabled |
| P4-5 | New pick made | Timer resets for next picker |
| P4-6 | "Help me pick" (client-side fallback) | Returns 3 suggestions with reasons, no network needed |
| P4-7 | "Help me pick" with backend available | Calls `POST /api/draft/suggest`, shows Claude suggestions |
| P4-8 | Claude API returns 429 | Seamlessly falls back to client-side suggestions |
| P4-9 | Draft complete (all 11 picks per user) | Draft room shows "Draft Complete" state |

### Phase 5 — E2E / deployment tests

| # | Test | Expected |
|---|------|----------|
| P5-1 | `firebase deploy --only hosting` | Site loads at Firebase Hosting URL |
| P5-2 | `/api/health` on Cloud Run URL | 200 from deployed Go service |
| P5-3 | Sign in on real phone (iOS Safari) | Full flow works, no layout issues |
| P5-4 | Sign in on Android Chrome | Full flow works |
| P5-5 | Add to home screen (PWA) | App icon appears, launches full-screen |
| P5-6 | Mock draft with 3 real users on phones | Real-time updates work, timer syncs, picks register |
| P5-7 | Process scores after mock match | Leaderboard updates within seconds |

---

## Key Conventions (for any agent continuing this work)

### Frontend
- **Mobile-first**, 375px base, max-width 480px (`var(--max-width)`)
- **Dark mode default** — all colors via CSS custom properties in `:root`
- **Alpine.js only** — no other JS frameworks, no build step
- All pages load: `js/app.js` first (deferred), then Alpine.js CDN (deferred)
- `Alpine.store('auth', {...})` is the global auth state — shape must stay stable
- Mock data lives in `js/app.js` as `const MOCK_*` globals — replaced by API calls in Phase 3
- CSS classes to know: `.card`, `.player-card`, `.leaderboard-card`, `.badge`, `.badge-gk/def/mid/fwd`, `.btn`, `.btn-primary/secondary/success/danger`, `.tabs`, `.tab-btn`, `.timer`, `.timer-green/yellow/red`, `.toast`

### Backend (Phase 2+)
- **Go** with `chi` router (or `net/http` ServeMux)
- **Structured JSON logging** to stdout (Cloud Run compatible)
- **All config via env vars** — no hardcoded secrets
- Firestore emulator: set `FIRESTORE_EMULATOR_HOST=127.0.0.1:8080` (use IP, not `localhost` — IPv6 vs IPv4 mismatch)
- Do NOT set `FIREBASE_AUTH_EMULATOR_HOST` — real Firebase Auth verifies tokens; emulator tokens are incompatible
- Auth: extract `Authorization: Bearer <token>`, verify with Firebase Admin SDK
- Context propagation on all Firestore/auth calls
- Admin check: read `users/{uid}.isAdmin` from Firestore after token verification

### Firestore collections
- `users/{uid}` — displayName, email, photoURL, teamName, createdAt, isAdmin
- `players/{playerId}` — name, country, position, clubTeam, rating, drafted, draftedBy, totalPoints
- `draft/config` — status, currentPickIndex, pickOrder[], pickTimeLimitSeconds, currentPickDeadline, round
- `draft/picks/{pickNumber}` — userId, playerId, playerName, round, pickNumber, timestamp
- `rosters/{uid}` — userId, teamName, players[], totalPoints
- `matches/{matchId}` — homeTeam, awayTeam, scores, stage, date, status, scoringProcessed
- `playerMatchStats/{playerId}_{matchId}` — goals, assists, cleanSheet, yellowCards, redCards, teamWin, pointsAwarded
- `leaderboard/current` — standings[] sorted array

---

## Environment Variables (Phase 2+)

```
FIREBASE_PROJECT_ID=andy-personal-1bb38
GOOGLE_APPLICATION_CREDENTIALS=/Users/andrewmartin/keys/fantasy-wc-sa.json  # local only
FIRESTORE_EMULATOR_HOST=127.0.0.1:8080   # local only — use 127.0.0.1 not localhost!
# FIREBASE_AUTH_EMULATOR_HOST must NOT be set — Go server uses real Firebase Auth
ANTHROPIC_API_KEY=sk-ant-...             # Phase 4.7
PORT=8081                                # local dev (8080 is Firestore emulator)
PORT=8080                                # Cloud Run sets this automatically
```

All local env vars are set by `dev.sh` — just run `./dev.sh` instead of `go run ./cmd/server`.
