# World Cup 2026 Fantasy Soccer App

## Overview

A mobile-first fantasy soccer web app for a small friend group. Users sign in with Google, participate in a live snake draft, and track standings throughout the 2026 World Cup.

**Tech Stack:**
- Frontend: HTML/CSS/Alpine.js (static site on Firebase Hosting)
- Backend: Go API on Google Cloud Run
- Database: Firestore (Native mode)
- Auth: Firebase Auth (Google Sign-In)
- Real-time: Firestore client-side listeners (draft only)
- Scoring Data: API-Football or football-data.org

**Architecture:**

```
Mobile Browser → Firebase Hosting (static HTML/CSS/JS + Alpine.js)
                        ↓
              Firebase Auth (Google Sign-In)
                        ↓
              Cloud Run (Go API) ← verifies Firebase ID tokens
                        ↓
                    Firestore
                        ↑
              Firebase Realtime Listeners (draft only, frontend → Firestore direct)
```

---

## Firestore Data Model

### Collection: `users`
```
users/{uid}
├── displayName: string        // from Google profile
├── email: string
├── photoURL: string
├── teamName: string           // user-chosen fantasy team name
├── createdAt: timestamp
└── isAdmin: bool
```

### Collection: `players`
```
players/{playerId}
├── name: string
├── country: string
├── position: string           // "GK", "DEF", "MID", "FWD"
├── clubTeam: string
├── rating: int                // static pre-draft ranking (1-100) for suggestions
├── imageURL: string           // optional
├── drafted: bool
├── draftedBy: string | null   // uid of user who drafted them
└── totalPoints: int
```

### Collection: `draft`
```
draft/config
├── status: string             // "pending", "active", "complete"
├── currentPickIndex: int      // index into the pick order array
├── pickOrder: []string        // array of uids in snake draft order
├── pickTimeLimitSeconds: int  // e.g. 90
├── currentPickDeadline: timestamp | null
└── round: int

draft/picks/{pickNumber}
├── userId: string
├── playerId: string
├── playerName: string
├── round: int
├── pickNumber: int
└── timestamp: timestamp
```

### Collection: `rosters`
```
rosters/{uid}
├── userId: string
├── teamName: string
├── players: []                // array of player references
│   ├── playerId: string
│   ├── name: string
│   ├── country: string
│   └── position: string
└── totalPoints: int
```

### Collection: `matches`
```
matches/{matchId}
├── homeTeam: string
├── awayTeam: string
├── homeScore: int
├── awayScore: int
├── stage: string              // "group", "r16", "quarter", "semi", "final"
├── date: timestamp
├── status: string             // "upcoming", "live", "complete"
└── scoringProcessed: bool
```

### Collection: `playerMatchStats`
```
playerMatchStats/{playerId}_{matchId}
├── playerId: string
├── matchId: string
├── goals: int
├── assists: int
├── cleanSheet: bool
├── yellowCards: int
├── redCards: int
├── teamWin: bool
└── pointsAwarded: int
```

### Collection: `leaderboard`
```
leaderboard/current
└── standings: []              // sorted array, updated after each scoring run
    ├── userId: string
    ├── teamName: string
    ├── displayName: string
    ├── photoURL: string
    └── totalPoints: int
```

---

## Scoring System

| Event | Points |
|---|---|
| Goal scored | +5 |
| Assist | +3 |
| Clean sheet (GK/DEF only) | +4 |
| Yellow card | -1 |
| Red card | -3 |
| Player's national team wins | +2 |
| Player's national team draws | +1 |

---

## Go API Endpoints

### Public (no auth)
- `GET /api/leaderboard` — returns current standings
- `GET /api/team/{uid}` — returns a user's roster and points breakdown
- `GET /api/draft/status` — returns current draft state (for spectators)

### Authenticated (requires Firebase ID token)
- `POST /api/user/register` — creates user doc after first Google sign-in, sets team name
- `PUT /api/user/team-name` — update fantasy team name
- `POST /api/draft/pick` — submit a draft pick (validates turn, availability, position limits)
- `POST /api/draft/suggest` — get AI-powered draft suggestions (falls back to context-aware if Anthropic API is unavailable)

### Admin (requires Firebase ID token + isAdmin flag)
- `POST /api/admin/draft/start` — begins the draft
- `POST /api/admin/draft/pause` — pauses draft
- `POST /api/admin/draft/resume` — resumes draft
- `POST /api/admin/players/import` — bulk import player list
- `POST /api/admin/scores/process` — trigger scoring for a match
- `PUT /api/admin/match/{matchId}` — manually add/edit match result
- `POST /api/admin/draft/set-order` — set or randomize draft order

---

## Snake Draft Logic

For N users, the draft order reverses each round:
- Round 1: User 1 → User 2 → ... → User N
- Round 2: User N → ... → User 2 → User 1
- Round 3: same as Round 1
- Continue until rosters are full

Roster size: 11 players (1 GK, 4 DEF, 4 MID, 2 FWD)

So the draft runs for 11 rounds × N users total picks.

Pick timer: 90 seconds per pick. If timer expires, auto-skip (admin can manually assign later) or auto-pick highest-ranked available player.

---

## Frontend Pages

1. **Landing / Leaderboard** (`index.html`) — default view, public, mobile-first cards
2. **Team View** (`team.html?id={uid}`) — tap a user to see their roster
3. **Draft Room** (`draft.html`) — live draft interface, auth required
4. **My Team** (`my-team.html`) — authenticated user's own roster view
5. **Admin Panel** (`admin.html`) — manage draft, import players, process scores
6. **Sign In** (`login.html`) — Google sign-in page

---

## Project Phases & Agent Tasks

Each task below is scoped to be a self-contained unit of work that can be handed to a Sonnet agent. Tasks within a phase can often be done in parallel. Tasks are numbered for dependency tracking.

---

### Phase 1 — Static Frontend (no backend, hardcoded data)

**Goal:** Get the mobile UI looking great with fake data. Pure HTML/CSS/Alpine.js.

#### Task 1.1 — Project scaffolding
> Create the project directory structure. Set up `index.html`, `draft.html`, `team.html`, `my-team.html`, `admin.html`, `login.html`. Create a shared `css/styles.css` and `js/app.js`. Add the Alpine.js CDN script tag to all pages. Add a shared nav header component. Use a mobile-first responsive layout (max-width: 480px primary, graceful desktop).

#### Task 1.2 — Leaderboard page
> Build `index.html` as the leaderboard. Use Alpine.js `x-data` with hardcoded mock standings (8 users). Render a ranked list of cards showing: rank number, user avatar (placeholder circle with initials), team name, display name, and total points. The #1 card should be visually distinct (gold accent). Cards should be tappable (link to `team.html?id=xxx`). Design should feel like a sports app — clean, bold typography, high contrast. Dark mode default.

#### Task 1.3 — Team view page
> Build `team.html`. Parse `id` from URL query param. Use Alpine.js with hardcoded mock roster data. Show team name and owner at the top, total points, then a list of players grouped by position (GK, DEF, MID, FWD). Each player card shows: name, country flag emoji, position badge, and points contributed. Mobile-first layout.

#### Task 1.4 — Draft room UI (static mockup)
> Build `draft.html` with a static mockup of the draft interface. Show: a top bar with "Round 2 — Pick 5" and a countdown timer (fake, not functional). Below that, a banner showing whose turn it is. Then a list of available players that can be filtered by position (tabs: ALL / GK / DEF / MID / FWD). Each player card shows name, country, position, and a "Draft" button. On the right side (or bottom on mobile), show the draft history as a scrollable log of picks. Use Alpine.js for the position filter tabs.

#### Task 1.5 — Login page and auth placeholder
> Build `login.html` with a centered card showing the app name/logo, a "Sign in with Google" button (non-functional placeholder), and a brief tagline. Should look polished. Also create a simple nav state in Alpine.js that shows "Sign In" link when logged out and user avatar + name when logged in (toggle with a hardcoded flag for now).

#### Task 1.6 — Admin panel mockup
> Build `admin.html` with sections for: draft controls (Start/Pause/Resume buttons), a match results form (home team, away team, scores, submit), a player import area (textarea for JSON or CSV paste), and a draft order display (list of users, drag to reorder or just display). All non-functional, just the UI. Gate behind a simple "Enter admin password" prompt using Alpine.js `x-show`.

---

### Phase 2 — Go API + Firestore

**Goal:** Build the backend. All endpoints working, tested locally, connected to Firestore.

#### Task 2.1 — Go project scaffolding
> Set up a Go module (`go mod init`). Create directory structure: `cmd/server/main.go`, `internal/handlers/`, `internal/middleware/`, `internal/models/`, `internal/firestore/`. Set up a basic HTTP server using `net/http` with a router (use `http.ServeMux` or a lightweight router like `chi`). Add a health check endpoint `GET /api/health`. Add CORS middleware that allows requests from your Firebase Hosting domain and `localhost`. Create a `Dockerfile` for Cloud Run deployment.

#### Task 2.2 — Firestore client setup
> Create a Firestore client wrapper in `internal/firestore/client.go`. Initialize the client using the Firebase Admin SDK for Go. Support both local development (using `FIRESTORE_EMULATOR_HOST` env var) and production (using default credentials on Cloud Run). Write helper functions for common operations: `GetDoc`, `SetDoc`, `UpdateDoc`, `QueryCollection`. Include proper error handling and context propagation.

#### Task 2.3 — Firebase Auth middleware
> Create auth middleware in `internal/middleware/auth.go`. Extract the `Authorization: Bearer <token>` header. Verify the Firebase ID token using the Firebase Admin SDK. Attach the decoded user info (UID, email, name) to the request context. Create a second middleware `RequireAdmin` that checks the user's Firestore `isAdmin` flag. Unauthenticated requests to protected routes return 401. Non-admin requests to admin routes return 403.

#### Task 2.4 — User endpoints
> Implement `POST /api/user/register` and `PUT /api/user/team-name`. Register should create the user doc in Firestore if it doesn't exist (idempotent). It should pull display name, email, and photo from the Firebase token claims and let the user set a team name. Team name update should validate the name isn't empty and isn't already taken. Both require auth middleware.

#### Task 2.5 — Player import endpoint
> Implement `POST /api/admin/players/import`. Accepts a JSON array of player objects. Validates required fields (name, country, position). Writes to the `players` collection in batch. Position must be one of GK/DEF/MID/FWD. Should be idempotent — if a player with the same name + country exists, update rather than duplicate. Requires admin middleware.

#### Task 2.6 — Leaderboard and team endpoints
> Implement `GET /api/leaderboard` (reads from `leaderboard/current` doc) and `GET /api/team/{uid}` (reads from `rosters/{uid}` and joins with player points). Both are public, no auth required. Return JSON. Leaderboard should return the sorted standings array. Team endpoint should return the roster with per-player point breakdowns.

#### Task 2.7 — Draft endpoints
> Implement the draft management endpoints. `POST /api/admin/draft/set-order` takes an array of UIDs or a flag to randomize. `POST /api/admin/draft/start` sets status to active and initializes the first pick. `POST /api/admin/draft/pause` and `/resume` toggle draft status. `GET /api/draft/status` returns the current draft state (public). `POST /api/draft/pick` is the core — validates it's the caller's turn, the player is available, the roster position isn't full, then writes the pick, updates the player as drafted, updates the roster, and advances the pick index (handling snake order reversal). Requires auth.

#### Task 2.8 — Scoring endpoints
> Implement `PUT /api/admin/match/{matchId}` to create/update match results. Implement `POST /api/admin/scores/process` which takes a matchId, looks up all drafted players from the teams in that match, applies the scoring rules based on `playerMatchStats`, updates each player's `totalPoints`, updates each roster's `totalPoints`, and rebuilds the `leaderboard/current` document. This should be idempotent — reprocessing a match replaces previous scores for that match.

---

### Phase 3 — Connect Frontend to Backend + Auth

**Goal:** Replace all hardcoded data with live API calls. Wire up Firebase Auth.

#### Task 3.1 — Firebase project setup guide
> Write a step-by-step guide (as a markdown file in the repo) for: creating a Firebase project, enabling Firestore in Native mode, enabling Firebase Auth with Google sign-in provider, enabling Firebase Hosting, installing the Firebase CLI, and getting the Firebase config object for the frontend. Include the `firebase.json` hosting config that rewrites API calls to Cloud Run.

#### Task 3.2 — Firebase Auth integration
> Add the Firebase JS SDK to the frontend (CDN). Initialize Firebase Auth in a shared `js/auth.js` module. Implement Google Sign-In on `login.html` using `signInWithPopup`. On successful sign-in, call `POST /api/user/register` with the ID token. Store the auth state in an Alpine.js global store (`Alpine.store('auth', {...})`) so all pages can access the current user and token. Implement sign-out. Redirect unauthenticated users away from protected pages.

#### Task 3.3 — API client module
> Create `js/api.js` — a thin wrapper around `fetch` for calling your Go API. It should automatically attach the Firebase ID token as a Bearer token on authenticated requests. Handle common error cases (401 → redirect to login, 500 → show error toast). Export functions like `getLeaderboard()`, `getTeam(uid)`, `submitPick(playerId)`, `register(teamName)`, etc. All functions return parsed JSON.

#### Task 3.4 — Wire up leaderboard
> Replace the hardcoded data in `index.html` with a call to `getLeaderboard()` via the API client. Use Alpine.js `x-init` to fetch on page load. Add a loading skeleton state while data is fetching. Handle empty state (no data yet). Auto-refresh every 60 seconds during tournament.

#### Task 3.5 — Wire up team view
> Replace hardcoded data in `team.html` with a call to `getTeam(uid)`. Parse UID from URL. Show loading state. Handle "team not found" gracefully.

#### Task 3.6 — Wire up admin panel
> Connect all admin panel controls to their respective API endpoints. Draft controls call start/pause/resume. Match form calls the match update endpoint. Player import calls the bulk import endpoint. Show success/error feedback for each action. Require admin auth.

---

### Phase 4 — Live Draft

**Goal:** Make the draft real-time and fully functional.

#### Task 4.1 — Firestore real-time listeners for draft
> In `draft.html`, set up Firestore `onSnapshot` listeners on `draft/config` and the `draft/picks` collection. When `draft/config` changes (new pick index, new round), update the Alpine.js state to reflect whose turn it is, the current round, and restart the countdown timer. When a new pick document appears, add it to the draft history log and remove the player from the available list. All of this should work automatically — no polling, no refresh needed.

#### Task 4.2 — Pick submission flow
> When the authenticated user clicks "Draft" on a player card, call `POST /api/draft/pick` with the player ID via the API client. Disable the button and show a loading spinner during the request. On success, the Firestore listener will automatically update the UI. On failure (not your turn, player taken, etc.), show an error message. Only show the "Draft" buttons when it's the current user's turn.

#### Task 4.3 — Countdown timer
> Build a countdown timer component in Alpine.js. It reads `currentPickDeadline` from the draft config (via Firestore listener). Counts down in real time. Visual urgency: green > 30s, yellow 10-30s, red < 10s. When it hits zero, show "Time's up!" and disable pick buttons. The Go API handles the actual timeout logic — the frontend timer is just a visual indicator.

#### Task 4.4 — Draft history and roster builder
> Build the draft log sidebar/bottom panel. Shows all picks in order: "Round 1, Pick 3: [User] drafted [Player] ([Country], [Position])". Scrolls to latest automatically. Also show the current user's roster-in-progress — which positions are filled, which still need picks, as a mini summary card at the top of the draft page.

#### Task 4.5 — Player search and filtering
> Enhance the available players list on the draft page. Add position filter tabs (ALL / GK / DEF / MID / FWD). Add a text search input that filters by player name or country. Show total available count per position. Gray out or remove already-drafted players. Sort by country alphabetically by default. This should all be client-side filtering using Alpine.js on the already-loaded player list.

#### Task 4.6 — Context-aware suggestion engine (client-side fallback)
> Build a client-side suggestion function in Alpine.js that analyzes the current user's roster and returns ranked recommendations from the available player pool. Logic should consider: positions still needed (if 0 GK picked and fewer than 3 rounds left, urgently suggest GK), country diversity (flag rosters too concentrated on one national team — if eliminated, you lose half your squad), positional balance across remaining rounds (e.g. "you have 6 picks left and still need 1 GK and 2 DEF, prioritize those"), and the player's static `rating` field as a tiebreaker. Return the top 3 suggestions with a short reason string for each (e.g. "You still need a goalkeeper" or "Diversifies your squad — no South American players yet"). Display these in a collapsible "Suggestions" card on the draft page, triggered by a "Help me pick" button. This must work fully offline from any external API — it's the fallback engine.

#### Task 4.7 — Claude-powered draft advisor (Go API + Anthropic API)
> Add a new endpoint `POST /api/draft/suggest` (authenticated). When called, the Go API gathers context: the user's current roster, all available players, current round/pick number, total rounds remaining, and a summary of what other users have drafted (countries/positions taken). Build a prompt template that sends this context to the Anthropic Messages API (`claude-sonnet-4-20250514`) and asks for the top 3 player recommendations with brief, conversational explanations for each. The prompt should instruct Claude to consider: roster needs, country diversity, group stage matchups, team strength, and draft position strategy (e.g. "you pick last next round so this position might dry up"). Return the suggestions as JSON to the frontend. Include a `max_tokens` of 500 to keep responses tight and costs low. Rate-limit to 1 suggestion request per pick per user to control token usage. On the frontend, the "Help me pick" button calls this endpoint first. If it returns a 429 (rate limit from Anthropic), 500, or any error, seamlessly fall back to the client-side context-aware suggestions from Task 4.6 without the user noticing a degraded experience — just swap in the local suggestions with the same UI treatment. Store the Anthropic API key as a Cloud Run environment variable, never expose it to the frontend.

---

### Phase 5 — Deployment & Polish

**Goal:** Deploy everything, test end-to-end, polish the experience.

#### Task 5.1 — Cloud Run deployment
> Write a deployment script or instructions for deploying the Go API to Cloud Run. Include: building the Docker image, pushing to Artifact Registry, deploying to Cloud Run with the correct environment variables (Firebase project ID, etc.), and setting up the service account with Firestore access. Verify the health check endpoint works.

#### Task 5.2 — Firebase Hosting deployment
> Configure `firebase.json` for hosting the static frontend. Set up URL rewrites so `/api/*` requests proxy to your Cloud Run service (or configure the frontend to call the Cloud Run URL directly with CORS). Run `firebase deploy --only hosting`. Verify the site loads and API calls work.

#### Task 5.3 — Player data seeding
> Source the full 2026 World Cup player data. Write a Go script or admin API call that seeds the `players` collection with all available players, their countries, positions, and club teams. This can be a manual process using the admin import endpoint — the key is having a clean JSON or CSV source of player data ready before draft night.

#### Task 5.4 — Scoring automation (stretch)
> Set up a Cloud Scheduler job that triggers scoring after each match day. Or, build a simple manual flow where you (admin) click "Process scores" after each match day and manually enter stats or pull them from an API. If using API-Football, write a Go function that fetches match results and player stats, maps them into `playerMatchStats` docs, and triggers the scoring processor.

#### Task 5.5 — Mobile polish and PWA
> Audit the entire frontend on a real phone. Fix any overflow issues, font sizes, tap targets. Add a `manifest.json` and service worker to make it installable as a PWA (add to home screen). Add a proper app icon and splash screen. Test on both iOS Safari and Android Chrome. This makes it feel like a real app for your friends.

#### Task 5.6 — Pre-draft-night testing
> End-to-end test with 2-3 friends before the real draft. Create test accounts, run a mock draft with a small player pool, verify: sign-in works, picks submit correctly, real-time updates work across devices, timer counts down, leaderboard updates after scoring. Document any bugs. This is your dress rehearsal.

---

## Notes for Agents

Each task should:
- Be completed in its own branch
- Include any new dependencies in `go.mod` or as CDN links
- Include comments explaining non-obvious decisions
- Not break existing functionality from prior tasks
- Include basic error handling (no silent failures)

Frontend conventions:
- Mobile-first (design for 375px width, scale up)
- Dark mode default, clean/sporty aesthetic
- Alpine.js for all reactivity, no other JS frameworks
- CSS custom properties for theming (colors, spacing)
- No build step — all files served as-is

Backend conventions:
- Standard library `net/http` or `chi` router
- Context propagation for all Firestore/auth operations
- Structured logging (JSON to stdout for Cloud Run)
- Environment variables for all config (no hardcoded secrets)
- Firestore emulator for local development
