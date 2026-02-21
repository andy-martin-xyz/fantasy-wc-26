package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/andrewmartin/fantasy-league/internal/models"
)

// GetLeaderboard handles GET /api/leaderboard — public, no auth.
func (h *Handler) GetLeaderboard(w http.ResponseWriter, r *http.Request) {
	var lb models.Leaderboard
	found, err := h.DB.GetDoc(r.Context(), "leaderboard", "current", &lb)
	if err != nil {
		h.Log.Error("get leaderboard", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !found {
		// Return empty leaderboard before any scores are processed.
		writeJSON(w, http.StatusOK, models.Leaderboard{Standings: []models.LeaderboardEntry{}})
		return
	}
	writeJSON(w, http.StatusOK, lb)
}

// GetTeam handles GET /api/team/{uid} — public, no auth.
// Returns the user's roster with per-player point breakdowns.
type teamResponse struct {
	UserID      string              `json:"userId"`
	TeamName    string              `json:"teamName"`
	TotalPoints int                 `json:"totalPoints"`
	Players     []playerWithPoints  `json:"players"`
}

type playerWithPoints struct {
	models.RosterPlayer
	TotalPoints int    `json:"totalPoints"`
	ClubTeam    string `json:"clubTeam"`
}

func (h *Handler) GetTeam(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		writeError(w, http.StatusBadRequest, "uid is required")
		return
	}

	var roster models.Roster
	found, err := h.DB.GetDoc(r.Context(), "rosters", uid, &roster)
	if err != nil {
		h.Log.Error("get team: roster", "uid", uid, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "team not found")
		return
	}

	// Enrich each roster player with their totalPoints from the players collection.
	enriched := make([]playerWithPoints, 0, len(roster.Players))
	for _, rp := range roster.Players {
		var p models.Player
		found, err := h.DB.GetDoc(r.Context(), "players", rp.PlayerID, &p)
		if err != nil || !found {
			// Include player with 0 points if lookup fails.
			enriched = append(enriched, playerWithPoints{RosterPlayer: rp})
			continue
		}
		enriched = append(enriched, playerWithPoints{
			RosterPlayer: rp,
			TotalPoints:  p.TotalPoints,
			ClubTeam:     p.ClubTeam,
		})
	}

	writeJSON(w, http.StatusOK, teamResponse{
		UserID:      roster.UserID,
		TeamName:    roster.TeamName,
		TotalPoints: roster.TotalPoints,
		Players:     enriched,
	})
}
