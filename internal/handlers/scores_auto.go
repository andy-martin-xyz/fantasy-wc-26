package handlers

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/andrewmartin/fantasy-league/internal/espn"
	"github.com/andrewmartin/fantasy-league/internal/models"
)

// fixtureSyncInterval is how often the full ESPN fixture list is re-synced.
// Knockout fixtures resolve at most once a day (after the previous round's
// matches finish), so hourly is comfortably ahead of any kickoff while
// avoiding an ESPN call + full matches read on every few-minute tick.
const fixtureSyncInterval = time.Hour

// autoScoreState is stored at meta/autoScore and gates the fixture sync.
type autoScoreState struct {
	LastFixtureSync time.Time `firestore:"lastFixtureSync"`
}

// AutoScore handles POST /api/cron/scores/poll.
// Step 1: at most once per fixtureSyncInterval, syncs any ESPN fixtures not
//         yet in Firestore (so knockout matches appear as ESPN resolves real
//         team names).
// Step 2: for every unscored match whose kickoff has passed, fetches ESPN's
//         current state. In-progress ("in") matches are scored on every tick so
//         goals appear live; finished ("post") matches are scored once and
//         marked scoringProcessed so they skip future ticks.
// Idempotent — safe to call every few minutes.
func (h *Handler) AutoScore(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Phase 1: fixture sync, gated to fixtureSyncInterval. A missing state doc
	// decodes to the zero time, so the first tick always syncs. The timestamp
	// only advances on success, so a failed sync retries next tick.
	var st autoScoreState
	if _, err := h.DB.GetDoc(ctx, "meta", "autoScore", &st); err != nil {
		h.Log.Warn("auto-score: read sync state (treating as never synced)", "err", err)
	}
	imported, syncRan := 0, false
	if time.Since(st.LastFixtureSync) >= fixtureSyncInterval {
		var syncErr error
		imported, syncErr = h.syncMissingFixtures(ctx)
		if syncErr != nil {
			h.Log.Warn("auto-score: fixture sync failed (continuing to score)", "err", syncErr)
		} else {
			syncRan = true
			if err := h.DB.SetDoc(ctx, "meta", "autoScore", autoScoreState{LastFixtureSync: time.Now().UTC()}); err != nil {
				h.Log.Warn("auto-score: save sync state", "err", err)
			}
		}
	}

	// Phase 2: query only unscored matches instead of scanning the whole
	// collection. Live matches (state="in") reappear every tick; only a
	// post-game write sets scoringProcessed=true so they drop out of the query.
	now := time.Now()
	docs, err := h.DB.FS.Collection("matches").
		Where("scoringProcessed", "==", false).
		Documents(ctx).GetAll()
	if err != nil {
		h.Log.Error("auto-score: list unscored matches", "err", err)
		writeError(w, http.StatusInternalServerError, "could not list matches")
		return
	}

	var todo []models.Match
	for _, d := range docs {
		var m models.Match
		if d.DataTo(&m) != nil {
			continue
		}
		if m.ESPNEventID == "" {
			continue
		}
		if !m.Date.Before(now) {
			continue // hasn't kicked off yet
		}
		todo = append(todo, m)
	}

	h.Log.Info("auto-score: candidates", "count", len(todo), "fixture_sync_ran", syncRan, "fixtures_imported", imported)

	if len(todo) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"fixture_sync_ran":  syncRan,
			"fixtures_imported": imported,
			"candidates":        0,
			"scored":            0,
			"results":           []any{},
		})
		return
	}

	// Phase 3: fetch ESPN summaries in parallel (max 5 concurrent calls).
	type espnResult struct {
		match models.Match
		sum   *espn.Summary
		err   error
	}
	fetched := make([]espnResult, len(todo))
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup
	for i, m := range todo {
		wg.Add(1)
		go func(i int, m models.Match) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			sum, err := espn.FetchSummary(ctx, m.ESPNEventID)
			if err != nil {
				fetched[i] = espnResult{match: m, err: err}
				h.Log.Warn("auto-score: espn fetch failed", "matchId", m.ID, "err", err)
				return
			}
			fetched[i] = espnResult{match: m, sum: sum}
		}(i, m)
	}
	wg.Wait()

	// Phase 4: score each match sequentially to avoid Firestore contention.
	// Recalculate the leaderboard after every write so goals appear live.
	type scoreResult struct {
		MatchID string `json:"matchId"`
		Home    string `json:"home"`
		Away    string `json:"away"`
		Status  string `json:"status"`
		Matched int    `json:"matched,omitempty"`
		Err     string `json:"error,omitempty"`
	}
	var results []scoreResult
	scored := 0

	for _, f := range fetched {
		m := f.match
		res := scoreResult{MatchID: m.ID, Home: m.HomeTeam, Away: m.AwayTeam}

		if f.err != nil {
			res.Status = "espn_error"
			res.Err = f.err.Error()
			results = append(results, res)
			continue
		}

		sum := f.sum
		if sum.State != "in" && sum.State != "post" {
			// "pre" or empty — ESPN not tracking yet even though kickoff passed.
			res.Status = "not_started"
			results = append(results, res)
			continue
		}

		out := matchOutcome(sum.State, sum.Home.Score, sum.Away.Score)

		// Same athlete→stats mapping as the admin fetch path; the cron ignores
		// preview rows and unmatched athletes (nobody is watching to fix them
		// mid-tick — the admin fetch endpoint surfaces those for review).
		var subs []statSubmission
		for _, s := range []struct {
			side       espn.TeamSide
			country    string
			win        bool
			cleanSheet bool
		}{
			{sum.Home, m.HomeTeam, out.homeWin, out.homeCleanSheet},
			{sum.Away, m.AwayTeam, out.awayWin, out.awayCleanSheet},
		} {
			ss, _, _, err := h.mapSideToStats(ctx, s.side, s.country, s.win, out.draw, s.cleanSheet)
			if err != nil {
				h.Log.Warn("auto-score: load players", "country", s.country, "err", err)
				continue
			}
			subs = append(subs, ss...)
		}

		if err := h.writeStats(ctx, m.ID, subs); err != nil {
			res.Status = "write_error"
			res.Err = err.Error()
			h.Log.Error("auto-score: write stats", "matchId", m.ID, "err", err)
			results = append(results, res)
			continue
		}

		// Update the match doc. Only set scoringProcessed on the final write.
		updates := []firestore.Update{
			{Path: "homeScore", Value: sum.Home.Score},
			{Path: "awayScore", Value: sum.Away.Score},
			{Path: "status", Value: statusFromState(sum.State)},
		}
		if sum.State == "post" {
			updates = append(updates, firestore.Update{Path: "scoringProcessed", Value: true})
		}
		_ = h.DB.UpdateDoc(ctx, "matches", m.ID, updates)

		if sum.State == "post" {
			res.Status = "scored"
		} else {
			res.Status = "live_scored"
		}
		res.Matched = len(subs)
		scored++
		h.Log.Info("auto-score: scored", "matchId", m.ID,
			"home", m.HomeTeam, "away", m.AwayTeam, "state", sum.State, "stats", len(subs))
		results = append(results, res)
	}

	// Recalculate once per poll tick (covering every match scored this tick),
	// rather than once per match, so leaderboard writes don't multiply with
	// concurrent live matches.
	if scored > 0 {
		if err := h.recalculateTotals(ctx); err != nil {
			h.Log.Error("auto-score: recalculate totals", "err", err)
		}
	}

	h.Log.Info("auto-score: complete", "candidates", len(todo), "scored", scored, "fixture_sync_ran", syncRan, "fixtures_imported", imported)
	writeJSON(w, http.StatusOK, map[string]any{
		"fixture_sync_ran":  syncRan,
		"fixtures_imported": imported,
		"candidates":        len(todo),
		"scored":            scored,
		"results":           results,
	})
}

// outcome holds the outcome-based scoring flags for one match state.
type outcome struct {
	homeWin, awayWin, draw         bool
	homeCleanSheet, awayCleanSheet bool
}

// matchOutcome derives win/draw/clean-sheet flags from an ESPN match state.
// Outcome points only exist once the match is final ("post") — a halftime
// lead isn't a win, and no goals conceded by the break isn't a clean sheet.
// For live ("in") matches every flag is false, so ticks award only event
// stats that have actually happened (goals, assists, cards); the final write
// adds the outcomes.
func matchOutcome(state string, homeScore, awayScore int) outcome {
	if state != "post" {
		return outcome{}
	}
	return outcome{
		homeWin:        homeScore > awayScore,
		awayWin:        awayScore > homeScore,
		draw:           homeScore == awayScore,
		homeCleanSheet: awayScore == 0,
		awayCleanSheet: homeScore == 0,
	}
}

// syncMissingFixtures fetches the full WC fixture list from ESPN and creates
// match docs for any events not yet in Firestore (updateExisting=false: known
// fixtures — including already-scored ones — are never touched). Placeholder
// team names ("Round of 32 Winner") are skipped until brackets resolve.
// Returns the number of new docs created.
func (h *Handler) syncMissingFixtures(ctx context.Context) (int, error) {
	fixtures, err := espn.FetchFixtures(ctx, wcTournamentDates)
	if err != nil {
		return 0, fmt.Errorf("fetch fixtures: %w", err)
	}
	res := h.upsertFixtures(ctx, fixtures, false)
	return res.Imported, nil
}
