# Simplification & Cost-Reduction Plan

> Written: 2026-07-10 — survey of the whole codebase (~8,700 lines of Go/JS/HTML).
> Ranked by importance: readability first, then operations cost, then dead weight.
> Nothing here is implemented yet; each item is independent unless noted.

---

## 1. Consolidate the three parallel scoring pipelines ⭐ biggest readability win

**Files:** `internal/handlers/scores.go`, `scores_auto.go`, `scores_fetch.go`

The same scoring logic exists three times with small variations, and they have
already started drifting (the cron path writes stats slightly differently than
the admin path):

| Duplicated logic | Copy 1 | Copy 2 |
|---|---|---|
| Write playerMatchStats + points | `applyMatchStats` step 1 (scores.go:96-122) | `writeMatchStatsOnly` (scores_auto.go:269-295) — near-verbatim copy |
| ESPN athlete → statSubmission mapping (`processSide` closure) | `FetchScores` (scores_fetch.go:61-109) | `AutoScore` (scores_auto.go:137-163) |
| Fixture import loop | `ImportFixtures` (fixtures.go:19-79) | `syncMissingFixtures` (scores_auto.go:223-265) |

**Plan:**
- Extract one `writeStats(ctx, matchID, subs)` helper; delete `writeMatchStatsOnly`.
- Extract one `mapSideToStats(side, country, win, draw, cleanSheet)` helper
  returning `[]statSubmission` (+ optional unmatched list); both FetchScores and
  AutoScore call it. FetchScores keeps its preview layer on top.
- Extract one `upsertFixtures(ctx, fixtures)` helper; `ImportFixtures` and
  `syncMissingFixtures` become thin wrappers.

**Payoff:** ~150 fewer lines, but mainly: one scoring behavior to reason about,
so a rule change (e.g. clean-sheet points) can't silently apply to only one path.
This is the highest-value item for a code review demo.

---

## 2. Cut AutoScore's fixed per-tick cost 💰 biggest steady-state ops win

**File:** `internal/handlers/scores_auto.go`

Every cron tick — all day, every few minutes, even when no matches are live — does:
1. `syncMissingFixtures`: one ESPN fixtures API call **and** a full `matches`
   collection read (~104 docs) to build the "already imported" set.
2. Phase 2: a **second** full `matches` collection read, filtered in memory to
   find unscored kicked-off matches.

**Plan (in order of value):**
- Replace the phase-2 full scan with a query:
  `Where("scoringProcessed", "==", false)`. Firestore reads then shrink from
  ~104 docs/tick to only the unscored残 matches (near zero late in the
  tournament). Single-field index, no composite needed if date filtering stays
  in memory.
- Share one matches read between sync and scoring instead of two.
- Gate fixture sync to a slow cadence (e.g. only when the tick actually scored
  something, or once/hour via a `lastFixtureSync` timestamp doc) — knockout
  bracket names change at most daily; polling ESPN's full schedule every few
  minutes buys nothing.

**Payoff:** ~2 full-collection reads + 1 external API call per tick → ~1 small
query per tick. At a few-minute cadence that's tens of thousands of document
reads per week removed. Pairs with the already-shipped batching work
(see PERFORMANCE_OPTIMIZATIONS.md).

---

## 3. Collapse GetTeam's N+1 queries 💰 biggest per-page-view ops win

**File:** `internal/handlers/leaderboard.go` (`GetTeam`)

Every team-page view currently costs: 1 roster read + **full `matches`
collection read** + per roster player (11×): 1 player GetDoc + 1
playerMatchStats query. ≈ 12 queries + ~115 doc reads per view, and it's a
public page the whole league refreshes during matches.

**Plan:**
- Batch the 11 player GetDocs into one `FS.GetAll` (same pattern already used
  in `recalculateAllRosterPoints`).
- Replace 11 playerMatchStats queries with **one** query using
  `Where("playerId", "in", playerIDs)` — Firestore `in` supports up to 30
  values; rosters are 11.
- Matches read is needed for opponent/date enrichment; keep it but it can be
  trimmed to referenced match IDs via `GetAll` on refs if desired.

**Payoff:** ~13 round trips → 3, meaningfully snappier page during live matches.

---

## 4. Delete the npm layer entirely 📦 easiest package removal

**Files:** `package.json`, `package-lock.json`, `node_modules/` (153 MB local)

The frontend loads Firebase from Google's CDN
(`https://www.gstatic.com/firebasejs/10.14.1/...-compat.js`) — the npm
`firebase` package is referenced by nothing. `node_modules` is untracked but
`package.json`/`package-lock.json` are committed and imply a build step that
doesn't exist.

**Plan:** `git rm package.json package-lock.json && rm -rf node_modules`.

**Payoff:** 153 MB reclaimed locally, and the repo stops advertising a phantom
Node dependency to anyone (or any CI) that sees it.

---

## 5. `go mod tidy` — drop unused Go dependencies 📦

**File:** `go.mod`

- `github.com/stretchr/testify` is a **direct requirement used by nothing**
  (the old mock-based test was its last consumer; the emulator rewrite uses
  only stdlib). `go mod tidy` removes it and its transitive `objx`/`spew`/
  `difflib` pins.
- `golang.org/x/text` is used directly (`scores_fetch.go` name normalization)
  but annotated `// indirect` — tidy fixes the annotation so the dependency
  list tells the truth.

**Payoff:** honest dependency graph; smaller module cache. Two minutes of work.

---

## 6. Archive the one-off data-prep tools 🧹 ~1,900 lines of dead weight

**Files:** `cmd/sofifa-test` (278), `cmd/apifootball-ids` (597),
`cmd/espn-ids` (352), `cmd/import` (538), `cmd/fixtures` (96)

These built the player pool before the tournament. The pool is loaded, the
tournament is running, and two of them are fully superseded by admin endpoints
(`cmd/import` → `POST /api/admin/players/import`, `cmd/fixtures` →
`POST /api/admin/fixtures/import`). `cmd/sofifa-test` is a scratch experiment
by its own name.

**Plan:** delete them (git history preserves everything). Keep `cmd/seed`
(still useful for local dev) and `cmd/server`. If deleting feels aggressive,
delete `sofifa-test` + `fixtures` + `import` now and keep the two ID-resolver
tools until the group stage ends.

**Revised outcome:** `sofifa-test` (scratch experiment) and `fixtures`
(genuinely redundant — its own header comment says "Mirrors the
`POST /api/admin/fixtures/import` handler") were deleted as planned.
`apifootball-ids` was also dropped, but for a different reason discovered
while doing this: its output file had 0/1244 players with an
`apiFootballId` ever resolved, and the model doesn't even have a field for
it — that pipeline never worked and nothing reads its output. `import` and
`espn-ids` were kept (real per-tournament bootstrap work, not reproducible
from an endpoint), moved to `tools/` to separate them from `cmd/`'s
deployed-service packages, and their three player-list JSON files
(`players.json`, `players-with-ids.json` — the dead one, `players-with-espn.json`)
consolidated into one `tools/import/players.json` that `espn-ids` now
enriches in place instead of writing a separate output file.

**Payoff:** ~22% of the Go codebase gone; `go build ./...`, CI, and every
future grep get faster and quieter.

---

## 7. Fix the CORS middleware — it currently allows every origin 🔒 + 💰

**File:** `cmd/server/main.go` (`corsMiddleware`)

Two issues in one function:
- `allowed := origin != ""` approves **any** origin that sends the header, so
  the `ALLOWED_ORIGIN` env var check below it is dead code. The comment says
  "dev-friendly" but this is production behavior.
- It logs every request at Debug, and the logger is configured at
  `slog.LevelDebug` with JSON to stdout — every request becomes a Cloud
  Logging ingestion line. Free tier is 50 GiB/month, but this is pure noise
  with a nonzero price during live-match traffic.

**Plan:** allow exactly `ALLOWED_ORIGIN` + localhost origins; drop the
per-request debug log (or raise the default log level to Info in production).
Auth is bearer-token (not cookies) so the risk is modest — but the code
currently *pretends* to restrict origins and doesn't, which is worse than
either honest option.

---

## 8. Reuse one Firebase app between Firestore and Auth 🧹

**Files:** `cmd/server/main.go`, `internal/db/firestore.go`

`db.New` builds a `firebase.App` to get the Firestore client; `main.go` then
builds a **second identical** `firebase.App` (same env-var/credential dance
duplicated) just to call `.Auth(ctx)`.

**Plan:** have `db.New` return (or expose) its app, and let main derive the
Auth client from it — deletes ~15 lines of duplicated init and one more place
credentials are wired.

---

## 9. Extract the duplicated nav/header across the 8 HTML pages 🧹 optional

**Files:** `public/*.html`

Every page carries a copied header/nav/hamburger block, and the big pages have
large inline scripts (draft 352 lines, admin 241, teams 149). No build step
exists, so full templating is out of scope — but a tiny `nav.js` that injects
the shared header (the pages already load shared JS files) removes the 8-way
copy that already bit once (the "fix hamburger nav" commit had to touch every
page).

Moving the large inline scripts to `public/js/draft.js` etc. is a nice-to-have
for editor tooling; zero behavior change. Lowest priority — do it next time
the nav changes rather than proactively.

---

## Explicitly considered and rejected

- **Splitting `db.Client` behind an interface** — the emulator-based tests
  made mocks unnecessary; an interface would add a layer with no consumer.
- **Framework/bundler for the frontend** — 8 static pages + Alpine via CDN is
  the right size for this app; a build step would add cost, not remove it.
- **Replacing chi** — three direct deps (chi, firestore, firebase) is already
  minimal; chi earns its keep in the route groups/middleware.
- **`internal/espn`** — 358 lines, single-purpose, well-shaped. Leave it.

## Suggested order of execution

1 (pipelines) → 2 (cron cost) → 5 (mod tidy, trivial) → 4 (npm) → 6 (cmd
cleanup) → 3 (GetTeam) → 7 (CORS) → 8 (firebase app) → 9 (frontend, deferred).

Items 4, 5, 6 are pure deletions and safe to batch into one commit. Items 1-3
each deserve their own commit with the emulator test suite run against them.
