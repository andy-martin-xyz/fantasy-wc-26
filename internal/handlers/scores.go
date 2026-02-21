package handlers

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"
	"github.com/go-chi/chi/v5"
	"github.com/andrewmartin/fantasy-league/internal/models"
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
	match.HomeTeam = strings.TrimSpace(match.HomeTeam)
	match.AwayTeam = strings.TrimSpace(match.AwayTeam)

	if match.HomeTeam == "" || match.AwayTeam == "" {
		writeError(w, http.StatusBadRequest, "homeTeam and awayTeam are required")
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

	// 1. Verify the match exists.
	var match models.Match
	found, err := h.DB.GetDoc(ctx, "matches", req.MatchID, &match)
	if err != nil || !found {
		writeError(w, http.StatusNotFound, "match not found — create it with PUT /api/admin/match/{id} first")
		return
	}

	// 2. Write playerMatchStats for each submitted player (overwrites previous).
	for _, s := range req.Stats {
		var player models.Player
		pfound, err := h.DB.GetDoc(ctx, "players", s.PlayerID, &player)
		if err != nil || !pfound {
			h.Log.Warn("process scores: player not found, skipping", "playerId", s.PlayerID)
			continue
		}

		stats := models.PlayerMatchStats{
			PlayerID:    s.PlayerID,
			MatchID:     req.MatchID,
			Goals:       s.Goals,
			Assists:     s.Assists,
			CleanSheet:  s.CleanSheet,
			YellowCards: s.YellowCards,
			RedCards:    s.RedCards,
			TeamWin:     s.TeamWin,
			TeamDraw:    s.TeamDraw,
		}
		stats.PointsAwarded = models.CalculatePoints(stats, player.Position)

		docID := s.PlayerID + "_" + req.MatchID
		if err := h.DB.SetDoc(ctx, "playerMatchStats", docID, stats); err != nil {
			h.Log.Error("process scores: write stats", "docId", docID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error writing stats")
			return
		}
	}

	// 3. Recalculate totalPoints for every drafted player.
	//    Sum all pointsAwarded across all their playerMatchStats docs.
	//    This is idempotent regardless of how many times we reprocess.
	if err := h.recalculateAllPlayerPoints(ctx); err != nil {
		h.Log.Error("process scores: recalculate player points", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error recalculating player points")
		return
	}

	// 4. Recalculate each roster's totalPoints.
	if err := h.recalculateAllRosterPoints(ctx); err != nil {
		h.Log.Error("process scores: recalculate roster points", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error recalculating roster points")
		return
	}

	// 5. Rebuild leaderboard/current.
	if err := h.rebuildLeaderboard(ctx); err != nil {
		h.Log.Error("process scores: rebuild leaderboard", "err", err)
		writeError(w, http.StatusInternalServerError, "error rebuilding leaderboard")
		return
	}

	// 6. Mark match as scored.
	_ = h.DB.UpdateDoc(ctx, "matches", req.MatchID, []firestore.Update{
		{Path: "scoringProcessed", Value: true},
		{Path: "status", Value: "complete"},
	})

	h.Log.Info("scores processed", "matchId", req.MatchID, "statsCount", len(req.Stats))
	writeJSON(w, http.StatusOK, map[string]any{"matchId": req.MatchID, "processed": len(req.Stats)})
}

// recalculateAllPlayerPoints sums each drafted player's match stats and updates totalPoints.
func (h *Handler) recalculateAllPlayerPoints(ctx context.Context) error {
	playerDocs, err := h.DB.FS.Collection("players").Where("drafted", "==", true).Documents(ctx).GetAll()
	if err != nil {
		return err
	}

	for _, playerDoc := range playerDocs {
		var player models.Player
		if err := playerDoc.DataTo(&player); err != nil {
			continue
		}

		statDocs, err := h.DB.FS.Collection("playerMatchStats").
			Where("playerId", "==", player.ID).
			Documents(ctx).GetAll()
		if err != nil {
			h.Log.Warn("recalculate: get player stats", "player", player.ID, "err", err)
			continue
		}

		total := 0
		for _, statDoc := range statDocs {
			var ps models.PlayerMatchStats
			if err := statDoc.DataTo(&ps); err == nil {
				total += ps.PointsAwarded
			}
		}

		if err := h.DB.UpdateDoc(ctx, "players", player.ID, []firestore.Update{
			{Path: "totalPoints", Value: total},
		}); err != nil {
			h.Log.Warn("recalculate: update player totalPoints", "player", player.ID, "err", err)
		}
	}
	return nil
}

// recalculateAllRosterPoints sums each roster player's totalPoints.
func (h *Handler) recalculateAllRosterPoints(ctx context.Context) error {
	rosterDocs, err := h.DB.FS.Collection("rosters").Documents(ctx).GetAll()
	if err != nil {
		return err
	}

	for _, rosterDoc := range rosterDocs {
		var roster models.Roster
		if err := rosterDoc.DataTo(&roster); err != nil {
			continue
		}
		total := 0
		for _, rp := range roster.Players {
			var player models.Player
			found, err := h.DB.GetDoc(ctx, "players", rp.PlayerID, &player)
			if err == nil && found {
				total += player.TotalPoints
			}
		}
		if err := h.DB.UpdateDoc(ctx, "rosters", rosterDoc.Ref.ID, []firestore.Update{
			{Path: "totalPoints", Value: total},
		}); err != nil {
			h.Log.Warn("recalculate: update roster totalPoints", "uid", rosterDoc.Ref.ID, "err", err)
		}
	}
	return nil
}

// rebuildLeaderboard reads all rosters, joins with user display names, sorts
// descending by totalPoints, and writes leaderboard/current.
func (h *Handler) rebuildLeaderboard(ctx context.Context) error {
	rosterDocs, err := h.DB.FS.Collection("rosters").Documents(ctx).GetAll()
	if err != nil {
		return err
	}

	standings := make([]models.LeaderboardEntry, 0, len(rosterDocs))
	for _, rosterDoc := range rosterDocs {
		var roster models.Roster
		if err := rosterDoc.DataTo(&roster); err != nil {
			continue
		}

		var user models.User
		found, err := h.DB.GetDoc(ctx, "users", roster.UserID, &user)
		if err != nil || !found {
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
