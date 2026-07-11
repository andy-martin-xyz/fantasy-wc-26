package handlers

import (
	"context"
	"fmt"
	"net/http"
	"sort"

	"cloud.google.com/go/firestore"
	"github.com/andrewmartin/fantasy-league/internal/models"
	"github.com/go-chi/chi/v5"
)

// UpsertMatch handles PUT /api/admin/match/{matchId}.
func (h *Handler) UpsertMatch(w http.ResponseWriter, r *http.Request) {
	matchID := chi.URLParam(r, "matchId")
	if matchID == "" {
		writeError(w, http.StatusBadRequest, "matchId is required")
		return
	}

	var match models.Match
	if err := decode(r, &match); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	match.ID = matchID
	match.HomeTeam = models.NormalizeTeamName(match.HomeTeam)
	match.AwayTeam = models.NormalizeTeamName(match.AwayTeam)

	if match.HomeTeam == "" || match.AwayTeam == "" {
		writeError(w, http.StatusBadRequest, "homeTeam and/or awayTeam not recognised — must be a WC 2026 qualified nation")
		return
	}

	if err := h.DB.SetDoc(r.Context(), "matches", matchID, match); err != nil {
		h.Log.Error("upsert match", "matchId", matchID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, match)
}

// ProcessScores handles POST /api/admin/scores/process.
// Accepts matchId + per-player stats. Writes stats, calculates points,
// updates player totals, roster totals, and rebuilds the leaderboard.
// Idempotent: re-running for the same match overwrites previous stats.
type processScoresRequest struct {
	MatchID string           `json:"matchId"`
	Stats   []statSubmission `json:"stats"`
}

type statSubmission struct {
	PlayerID    string `json:"playerId"`
	Goals       int    `json:"goals"`
	Assists     int    `json:"assists"`
	CleanSheet  bool   `json:"cleanSheet"`
	YellowCards int    `json:"yellowCards"`
	RedCards    int    `json:"redCards"`
	TeamWin     bool   `json:"teamWin"`
	TeamDraw    bool   `json:"teamDraw"`
}

func (h *Handler) ProcessScores(w http.ResponseWriter, r *http.Request) {
	var req processScoresRequest
	if err := decode(r, &req); err != nil || req.MatchID == "" {
		writeError(w, http.StatusBadRequest, "matchId and stats are required")
		return
	}

	ctx := r.Context()

	// Verify the match exists.
	var match models.Match
	found, err := h.DB.GetDoc(ctx, "matches", req.MatchID, &match)
	if err != nil || !found {
		writeError(w, http.StatusNotFound, "match not found — create it with PUT /api/admin/match/{id} first")
		return
	}

	if err := h.applyMatchStats(ctx, req.MatchID, req.Stats); err != nil {
		h.Log.Error("process scores", "matchId", req.MatchID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error processing scores")
		return
	}

	h.Log.Info("scores processed", "matchId", req.MatchID, "statsCount", len(req.Stats))
	writeJSON(w, http.StatusOK, map[string]any{"matchId": req.MatchID, "processed": len(req.Stats)})
}

// applyMatchStats writes per-player stats for a match, recomputes player,
// roster, and leaderboard totals, and marks the match complete. Idempotent:
// re-running for the same match overwrites previous stats. Shared by manual
// score entry (ProcessScores) and ESPN auto-fetch (FetchScores).
func (h *Handler) applyMatchStats(ctx context.Context, matchID string, stats []statSubmission) error {
	// 1. Write playerMatchStats for each submitted player (overwrites previous).
	for _, s := range stats {
		var player models.Player
		pfound, err := h.DB.GetDoc(ctx, "players", s.PlayerID, &player)
		if err != nil || !pfound {
			h.Log.Warn("apply stats: player not found, skipping", "playerId", s.PlayerID)
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

	// 2. Recalculate totalPoints for every drafted player (idempotent).
	if err := h.recalculateAllPlayerPoints(ctx); err != nil {
		return fmt.Errorf("recalculate player points: %w", err)
	}
	// 3. Recalculate each roster's totalPoints.
	if err := h.recalculateAllRosterPoints(ctx); err != nil {
		return fmt.Errorf("recalculate roster points: %w", err)
	}
	// 4. Rebuild leaderboard/current.
	if err := h.rebuildLeaderboard(ctx); err != nil {
		return fmt.Errorf("rebuild leaderboard: %w", err)
	}
	// 5. Mark match as scored.
	_ = h.DB.UpdateDoc(ctx, "matches", matchID, []firestore.Update{
		{Path: "scoringProcessed", Value: true},
		{Path: "status", Value: "complete"},
	})
	return nil
}

// recalculateAllPlayerPoints sums each drafted player's match stats and updates totalPoints.
// Reads playerMatchStats once (instead of once per drafted player) and groups
// in memory, so cost stays flat regardless of how many players are drafted.
func (h *Handler) recalculateAllPlayerPoints(ctx context.Context) error {
	playerDocs, err := h.DB.FS.Collection("players").Where("drafted", "==", true).Documents(ctx).GetAll()
	if err != nil {
		return err
	}

	statDocs, err := h.DB.FS.Collection("playerMatchStats").Documents(ctx).GetAll()
	if err != nil {
		return fmt.Errorf("get all player match stats: %w", err)
	}

	totals := make(map[string]int, len(playerDocs))
	for _, statDoc := range statDocs {
		var ps models.PlayerMatchStats
		if err := statDoc.DataTo(&ps); err == nil {
			totals[ps.PlayerID] += ps.PointsAwarded
		}
	}

	for _, playerDoc := range playerDocs {
		total := totals[playerDoc.Ref.ID]
		if err := h.DB.UpdateDoc(ctx, "players", playerDoc.Ref.ID, []firestore.Update{
			{Path: "totalPoints", Value: total},
		}); err != nil {
			h.Log.Warn("recalculate: update player totalPoints", "player", playerDoc.Ref.ID, "err", err)
		}
	}
	return nil
}

// recalculateAllRosterPoints sums each roster player's totalPoints.
// Fetches every referenced player doc in a single batched GetAll instead of
// one GetDoc per player-per-roster, so cost scales with unique players, not
// roster count × roster size.
func (h *Handler) recalculateAllRosterPoints(ctx context.Context) error {
	rosterDocs, err := h.DB.FS.Collection("rosters").Documents(ctx).GetAll()
	if err != nil {
		return err
	}

	rosters := make([]models.Roster, 0, len(rosterDocs))
	rosterIDs := make([]string, 0, len(rosterDocs))
	playerIDSet := make(map[string]bool)
	for _, rosterDoc := range rosterDocs {
		var roster models.Roster
		if err := rosterDoc.DataTo(&roster); err != nil {
			continue
		}
		rosters = append(rosters, roster)
		rosterIDs = append(rosterIDs, rosterDoc.Ref.ID)
		for _, rp := range roster.Players {
			playerIDSet[rp.PlayerID] = true
		}
	}

	playerRefs := make([]*firestore.DocumentRef, 0, len(playerIDSet))
	for id := range playerIDSet {
		playerRefs = append(playerRefs, h.DB.FS.Collection("players").Doc(id))
	}

	players := make(map[string]models.Player, len(playerRefs))
	if len(playerRefs) > 0 {
		snaps, err := h.DB.FS.GetAll(ctx, playerRefs)
		if err != nil {
			return fmt.Errorf("batch get players: %w", err)
		}
		for _, snap := range snaps {
			if !snap.Exists() {
				continue
			}
			var p models.Player
			if err := snap.DataTo(&p); err == nil {
				players[snap.Ref.ID] = p
			}
		}
	}

	for i, roster := range rosters {
		total := 0
		for _, rp := range roster.Players {
			if p, ok := players[rp.PlayerID]; ok {
				total += p.TotalPoints
			}
		}
		if err := h.DB.UpdateDoc(ctx, "rosters", rosterIDs[i], []firestore.Update{
			{Path: "totalPoints", Value: total},
		}); err != nil {
			h.Log.Warn("recalculate: update roster totalPoints", "uid", rosterIDs[i], "err", err)
		}
	}
	return nil
}

// rebuildLeaderboard reads all rosters, joins with user display names, sorts
// descending by totalPoints, and writes leaderboard/current.
// User docs are fetched in a single batched GetAll instead of one GetDoc per
// roster.
func (h *Handler) rebuildLeaderboard(ctx context.Context) error {
	rosterDocs, err := h.DB.FS.Collection("rosters").Documents(ctx).GetAll()
	if err != nil {
		return err
	}

	rosters := make([]models.Roster, 0, len(rosterDocs))
	userIDSet := make(map[string]bool)
	for _, rosterDoc := range rosterDocs {
		var roster models.Roster
		if err := rosterDoc.DataTo(&roster); err != nil {
			continue
		}
		rosters = append(rosters, roster)
		userIDSet[roster.UserID] = true
	}

	userRefs := make([]*firestore.DocumentRef, 0, len(userIDSet))
	for id := range userIDSet {
		userRefs = append(userRefs, h.DB.FS.Collection("users").Doc(id))
	}

	users := make(map[string]models.User, len(userRefs))
	if len(userRefs) > 0 {
		snaps, err := h.DB.FS.GetAll(ctx, userRefs)
		if err != nil {
			return fmt.Errorf("batch get users: %w", err)
		}
		for _, snap := range snaps {
			if !snap.Exists() {
				continue
			}
			var u models.User
			if err := snap.DataTo(&u); err == nil {
				users[snap.Ref.ID] = u
			}
		}
	}

	standings := make([]models.LeaderboardEntry, 0, len(rosters))
	for _, roster := range rosters {
		user, ok := users[roster.UserID]
		if !ok {
			continue
		}
		standings = append(standings, models.LeaderboardEntry{
			UserID:      roster.UserID,
			TeamName:    roster.TeamName,
			DisplayName: user.DisplayName,
			PhotoURL:    user.PhotoURL,
			TotalPoints: roster.TotalPoints,
		})
	}

	sort.Slice(standings, func(i, j int) bool {
		return standings[i].TotalPoints > standings[j].TotalPoints
	})

	lb := models.Leaderboard{Standings: standings}
	return h.DB.SetDoc(ctx, "leaderboard", "current", lb)
}
