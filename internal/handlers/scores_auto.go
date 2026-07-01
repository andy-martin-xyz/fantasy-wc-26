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

// AutoScore handles POST /api/cron/scores/poll.
// Step 1: syncs any ESPN fixtures not yet in Firestore (so knockout matches
//         appear as soon as ESPN shows real team names).
// Step 2: scores every unscored match that kicked off ≥ 2h30m ago.
// Idempotent — safe to call every few minutes.
func (h *Handler) AutoScore(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Phase 1: sync fixtures — import any ESPN events missing from Firestore.
	imported, syncErr := h.syncMissingFixtures(ctx)
	if syncErr != nil {
		h.Log.Warn("auto-score: fixture sync failed (continuing to score)", "err", syncErr)
	}

	// Phase 2: load all unscored matches that have had enough time to finish.
	docs, err := h.DB.FS.Collection("matches").Documents(ctx).GetAll()
	if err != nil {
		h.Log.Error("auto-score: list matches", "err", err)
		writeError(w, http.StatusInternalServerError, "could not list matches")
		return
	}

	cutoff := time.Now().Add(-(2*time.Hour + 30*time.Minute))
	var todo []models.Match
	for _, d := range docs {
		var m models.Match
		if d.DataTo(&m) != nil {
			continue
		}
		if m.ScoringProcessed || m.ESPNEventID == "" {
			continue
		}
		if !m.Date.Before(cutoff) {
			continue // not enough time elapsed since kickoff
		}
		todo = append(todo, m)
	}

	h.Log.Info("auto-score: matches to score", "count", len(todo), "fixtures_imported", imported)

	if len(todo) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
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
		skip  bool
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
			if !sum.Completed {
				fetched[i] = espnResult{match: m, skip: true}
				return
			}
			fetched[i] = espnResult{match: m, sum: sum}
		}(i, m)
	}
	wg.Wait()

	// Phase 4: write playerMatchStats + mark matches complete (sequential to
	// avoid concurrent Firestore contention on the shared scoring paths).
	type scoreResult struct {
		MatchID string `json:"matchId"`
		Home    string `json:"home"`
		Away    string `json:"away"`
		Status  string `json:"status"`
		Matched int    `json:"matched,omitempty"`
		Err     string `json:"error,omitempty"`
	}
	var results []scoreResult
	anyScored := false

	for _, f := range fetched {
		m := f.match
		res := scoreResult{MatchID: m.ID, Home: m.HomeTeam, Away: m.AwayTeam}

		switch {
		case f.err != nil:
			res.Status = "espn_error"
			res.Err = f.err.Error()
		case f.skip:
			res.Status = "not_finished"
		default:
			sum := f.sum
			draw := sum.Home.Score == sum.Away.Score
			homeWin := sum.Home.Score > sum.Away.Score
			awayWin := sum.Away.Score > sum.Home.Score

			var subs []statSubmission
			processSide := func(side espn.TeamSide, country string, win, cleanSheet bool) {
				cands, byESPN, err := h.loadCountryPlayers(ctx, country)
				if err != nil {
					h.Log.Warn("auto-score: load players", "country", country, "err", err)
					return
				}
				for _, a := range side.Athletes {
					if !a.Played {
						continue
					}
					pid, pos := "", ""
					if c, ok := byESPN[a.ESPNID]; ok && a.ESPNID != "" {
						pid, pos = c.ID, c.Position
					} else {
						pid, pos = matchAthlete(a.Name, cands)
					}
					if pid == "" {
						continue
					}
					cs := cleanSheet && (pos == "GK" || pos == "DEF")
					subs = append(subs, statSubmission{
						PlayerID: pid, Goals: a.Goals, Assists: a.Assists,
						YellowCards: a.Yellow, RedCards: a.Red,
						CleanSheet: cs, TeamWin: win, TeamDraw: draw,
					})
				}
			}
			processSide(sum.Home, m.HomeTeam, homeWin, sum.Away.Score == 0)
			processSide(sum.Away, m.AwayTeam, awayWin, sum.Home.Score == 0)

			// Write per-player stats without recalculating — we batch that at
			// the end so N matches only costs one full recalculate pass.
			if err := h.writeMatchStatsOnly(ctx, m.ID, subs); err != nil {
				res.Status = "write_error"
				res.Err = err.Error()
				h.Log.Error("auto-score: write stats", "matchId", m.ID, "err", err)
			} else {
				_ = h.DB.UpdateDoc(ctx, "matches", m.ID, []firestore.Update{
					{Path: "scoringProcessed", Value: true},
					{Path: "status", Value: "complete"},
					{Path: "homeScore", Value: sum.Home.Score},
					{Path: "awayScore", Value: sum.Away.Score},
				})
				res.Status = "scored"
				res.Matched = len(subs)
				anyScored = true
				h.Log.Info("auto-score: scored", "matchId", m.ID,
					"home", m.HomeTeam, "away", m.AwayTeam, "stats", len(subs))
			}
		}
		results = append(results, res)
	}

	// Phase 5: one leaderboard recalculate if anything changed.
	if anyScored {
		if err := h.recalculateAllPlayerPoints(ctx); err != nil {
			h.Log.Error("auto-score: recalculate players", "err", err)
		}
		if err := h.recalculateAllRosterPoints(ctx); err != nil {
			h.Log.Error("auto-score: recalculate rosters", "err", err)
		}
		if err := h.rebuildLeaderboard(ctx); err != nil {
			h.Log.Error("auto-score: rebuild leaderboard", "err", err)
		}
	}

	scored := 0
	for _, r := range results {
		if r.Status == "scored" {
			scored++
		}
	}
	h.Log.Info("auto-score: complete", "candidates", len(todo), "scored", scored, "fixtures_imported", imported)
	writeJSON(w, http.StatusOK, map[string]any{
		"fixtures_imported": imported,
		"candidates":        len(todo),
		"scored":            scored,
		"results":           results,
	})
}

// syncMissingFixtures fetches the full WC fixture list from ESPN and creates
// match docs for any events not yet in Firestore. Skips placeholder team names
// (e.g. "Round of 32 Winner") that appear before brackets are set, and never
// overwrites a match that's already been scored. Returns the number of new docs
// created.
func (h *Handler) syncMissingFixtures(ctx context.Context) (int, error) {
	fixtures, err := espn.FetchFixtures(ctx, "20260611-20260719")
	if err != nil {
		return 0, fmt.Errorf("fetch fixtures: %w", err)
	}

	// Build a set of match IDs already in Firestore.
	existing, err := h.DB.FS.Collection("matches").Documents(ctx).GetAll()
	if err != nil {
		return 0, fmt.Errorf("list matches: %w", err)
	}
	inDB := make(map[string]bool, len(existing))
	for _, d := range existing {
		inDB[d.Ref.ID] = true
	}

	imported := 0
	for _, f := range fixtures {
		if inDB[f.EventID] {
			continue // already have this match
		}
		home := models.NormalizeTeamName(f.HomeTeam)
		away := models.NormalizeTeamName(f.AwayTeam)
		if home == "" || away == "" {
			continue // placeholder — real teams not yet decided
		}
		match := models.Match{
			ID:          f.EventID,
			HomeTeam:    home,
			AwayTeam:    away,
			Date:        f.Date,
			Status:      statusFromState(f.State),
			ESPNEventID: f.EventID,
		}
		if err := h.DB.SetDoc(ctx, "matches", f.EventID, match); err != nil {
			h.Log.Warn("sync fixtures: write failed", "id", f.EventID, "err", err)
			continue
		}
		imported++
		h.Log.Info("sync fixtures: imported", "id", f.EventID, "home", home, "away", away, "date", f.Date.Format("Jan 02"))
	}
	return imported, nil
}

// writeMatchStatsOnly writes playerMatchStats docs for one match without
// triggering the recalculate cycle. AutoScore batches all recalculates at end.
func (h *Handler) writeMatchStatsOnly(ctx context.Context, matchID string, stats []statSubmission) error {
	for _, s := range stats {
		var player models.Player
		pfound, err := h.DB.GetDoc(ctx, "players", s.PlayerID, &player)
		if err != nil || !pfound {
			h.Log.Warn("write match stats: player not found, skipping", "playerId", s.PlayerID)
			continue
		}
		ps := models.PlayerMatchStats{
			PlayerID:    s.PlayerID,
			MatchID:     matchID,
			Goals:       s.Goals,
			Assists:     s.Assists,
			CleanSheet:  s.CleanSheet,
			YellowCards: s.YellowCards,
			RedCards:    s.RedCards,
			TeamWin:     s.TeamWin,
			TeamDraw:    s.TeamDraw,
		}
		ps.PointsAwarded = models.CalculatePoints(ps, player.Position)
		docID := s.PlayerID + "_" + matchID
		if err := h.DB.SetDoc(ctx, "playerMatchStats", docID, ps); err != nil {
			return fmt.Errorf("write stats %s: %w", docID, err)
		}
	}
	return nil
}
