# Firestore Cost & Performance Optimizations

> Started: 2026-07-10
> Context: Firestore billing showed unexpectedly high read/write counts. Traced
> to `AutoScore` (`/api/cron/scores/poll`), which reruns full-collection
> recalculation on every scored match instead of once per poll tick.

---

## 1. Recalculation moved out of the per-match loop

**File:** `internal/handlers/scores_auto.go`

**Before:** `recalculateAllPlayerPoints`, `recalculateAllRosterPoints`, and
`rebuildLeaderboard` were called inside the `for _, f := range fetched` loop —
once per match scored in a single poll tick. With N concurrent live matches,
the entire recalculation cascade ran N times per tick.

**After:** The three calls run once after the loop, guarded by `if scored > 0`.
One poll tick now does one recalculation pass covering every match scored that
tick, regardless of how many matches were live simultaneously.

**Impact:** Divides recalculation cost by the number of concurrent live
matches (2-3x during typical World Cup group-stage windows).

---

## 2. Batched player lookups in `recalculateAllRosterPoints`

**File:** `internal/handlers/scores.go`

**Before:** For every roster, looped over every player on that roster and
issued an individual `GetDoc` (`players/{id}`) — i.e. `O(rosters × roster size)`
reads (~30 rosters × 15 players ≈ 450 reads per call).

**After:** Collects all unique player IDs across every roster first, then
fetches them in a single `firestore.Client.GetAll` batched request. Reads now
scale with the number of *unique* players across all rosters, not
`rosters × roster size` — typically the drafted-player pool (~50-80), a
5-10x reduction.

---

## 3. Batched user lookups in `rebuildLeaderboard`

**File:** `internal/handlers/scores.go`

**Before:** One `GetDoc` (`users/{uid}`) per roster to join in display name/photo.

**After:** Same batching pattern — collect unique user IDs, single `GetAll` call.
Reads drop from `O(rosters)` individual round trips to 1 batched call.

---

## 4. Query flattened in `recalculateAllPlayerPoints`

**File:** `internal/handlers/scores.go`

**Before:** For every drafted player, ran a separate query:
`playerMatchStats.Where("playerId", "==", player.ID)`. Cost scaled with the
number of drafted players (~50 separate queries per call).

**After:** Fetches the entire `playerMatchStats` collection once, groups
totals by `playerId` in memory. Cost is now flat — one query regardless of
drafted-player count.

---

## Rough before/after estimate

Assumptions: 30 rosters × 15 players/roster, 60 drafted players, 3 concurrent
live matches in a poll tick.

| | Before | After |
|---|---|---|
| Roster recalculation reads | ~450 × 3 matches ≈ 1,350 | ~60 (unique players) |
| Leaderboard reads | ~30 × 3 ≈ 90 | ~30 |
| Player points reads | ~50 queries × 3 ≈ 150+ | 2 queries total |
| **Total per poll tick (3 live matches)** | **~1,600+ reads** | **~100 reads** |

Directionally an order-of-magnitude reduction per tick; exact savings depend
on roster size and how many matches are live simultaneously.

---

---

## 5. AutoScore idle-tick cost cut to near zero (branch: perf/reduce-autoscore-polling)

**File:** `internal/handlers/scores_auto.go`

**Before:** Every cron tick — even with nothing live — did an ESPN fixtures
API call plus **two** full `matches` collection reads (~101 docs each): one in
`syncMissingFixtures` to build the already-imported set, one to scan for
unscored matches.

**After:**
- Unscored matches come from `Where("scoringProcessed", "==", false)` — a
  single-field equality query (auto-indexed, no composite index needed) that
  returns only the handful of remaining fixtures instead of the whole
  collection.
- Fixture sync is gated to once per hour via a `meta/autoScore` state doc
  (1 doc read per tick). The timestamp only advances on a successful sync, so
  failures retry on the next tick. Knockout fixtures resolve at most daily, so
  hourly still imports the final/3rd-place fixtures days before kickoff.

**Impact:** idle tick drops from ~200+ doc reads + 1 external API call to
~4 doc reads and no ESPN call. Verified against production data first:
all 101 match docs carry the `scoringProcessed` field, so the query strands
nothing.

---

## Also fixed while in here: broken draft handler tests

Unrelated to Firestore cost, but found while verifying the changes above
didn't break anything: `internal/handlers/draft_test.go` didn't compile.
It used a hand-rolled `MockFirestoreClient` meant to substitute for
`Handler.DB`, but `Handler.DB` is a concrete `*db.Client` (not an interface),
so the mock could never satisfy it — the file was broken before this session.

It also wasn't fixable by just patching types: `draft.go`'s `SubmitPick`,
`UndoLastPick`, and `ResetDraft` use Firestore transactions, batches, and
subcollection queries directly via `h.DB.FS`, which isn't reasonably
mockable at this layer.

**Fix:** rewired the tests to run against the real Firestore emulator
(already configured in `firebase.json`) instead of a mock. Tests call
`t.Skip` if `FIRESTORE_EMULATOR_HOST` isn't set, so `go test ./...` stays
green with no emulator running; starting the emulator
(`firebase emulators:start --only firestore`) gets full coverage including
the transactional pick/undo logic. Verified both paths: 25/25 subtests pass
with the emulator running, and the suite skips cleanly without it.

---

## Ideas not yet implemented (future work)

- **Incremental deltas instead of full recalculation.** We know exactly which
  players' stats changed on a given tick — updating just their totals (and
  the rosters/leaderboard entries that reference them) instead of resumming
  every drafted player and every roster would cut reads further, at the cost
  of more complex bookkeeping (has to track "what changed" instead of
  recomputing everything from source data).
- **Cron frequency tuning.** Poll interval could back off outside of live
  match windows (e.g. hourly when nothing is scheduled `today`, vs. every
  few minutes when a match is `in` progress).
- **Firestore usage dashboards / budget alerts** to catch regressions before
  they show up on the monthly bill.
