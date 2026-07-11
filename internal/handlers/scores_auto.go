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
//         appear as soon as ESPN resolves real team names).
// Step 2: for every unscored match whose kickoff has passed, fetches ESPN's
//         current state. In-progress ("in") matches are scored on every tick so
//         goals appear live; finished ("post") matches are scored once and
//         marked scoringProcessed so they skip future ticks. The leaderboard
//         recalculates after every individual match write.
// Idempotent — safe to call every few minutes.
func (h *Handler) AutoScore(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Phase 1: sync fixtures — import any ESPN events missing from Firestore.
	imported, syncErr := h.syncMissingFixtures(ctx)
	if syncErr != nil {
		h.Log.Warn("auto-score: fixture sync failed (continuing to score)", "err", syncErr)
	}

	// Phase 2: load all unscored matches whose kickoff has passed.
	// Live matches (state="in") reappear every tick; only a post-game write
	// sets scoringProcessed=true so they drop out of future polls.
	now := time.Now()
	docs, err := h.DB.FS.Collection("matches").Documents(ctx).GetAll()
	if err != nil {
		h.Log.Error("auto-score: list matches", "err", err)
		writeError(w, http.StatusInternalServerError, "could not list matches")
		return
	}

	var todo []models.Match
	for _, d := range docs {
		var m models.Match
		if d.DataTo(&m) != nil {
			continue
		}
		if m.ScoringProcessed || m.ESPNEventID == "" {
			continue
		}
		if !m.Date.Before(now) {
			continue // hasn't kicked off yet
		}
		todo = append(todo, m)
	}

	h.Log.Info("auto-score: candidates", "count", len(todo), "fixtures_imported", imported)

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

		// Win/draw based on the current score so live win-bonus feedback kicks
		// in immediately. The final "post" write locks in the correct value.
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

		if err := h.writeMatchStatsOnly(ctx, m.ID, subs); err != nil {
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

// writeMatchStatsOnly writes playerMatchStats docs for one match.
// Callers are responsible for calling the recalculate helpers afterwards.
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
