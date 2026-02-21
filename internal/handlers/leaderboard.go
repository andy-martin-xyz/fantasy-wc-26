package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/andrewmartin/fantasy-league/internal/models"
)

// GetLeaderboard handles GET /api/leaderboard — public, no auth.
// Before any scores are processed, falls back to all registered users with 0 pts
// so the standings page shows who has signed up even before the draft.
func (h *Handler) GetLeaderboard(w http.ResponseWriter, r *http.Request) {
	var lb models.Leaderboard
	found, err := h.DB.GetDoc(r.Context(), "leaderboard", "current", &lb)
	if err != nil {
		h.Log.Error("get leaderboard", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if found && len(lb.Standings) > 0 {
		writeJSON(w, http.StatusOK, lb)
		return
	}

	// No standings yet (pre-draft) — show all registered users with 0 pts.
	docs, err := h.DB.FS.Collection("users").Documents(r.Context()).GetAll()
	if err != nil {
		h.Log.Error("get users for pre-draft standings", "err", err)
		writeJSON(w, http.StatusOK, models.Leaderboard{Standings: []models.LeaderboardEntry{}})
		return
	}
	standings := make([]models.LeaderboardEntry, 0, len(docs))
	for _, doc := range docs {
		var u models.User
		if err := doc.DataTo(&u); err != nil {
			continue
		}
		standings = append(standings, models.LeaderboardEntry{
			UserID:      u.UID,
			DisplayName: u.DisplayName,
			PhotoURL:    u.PhotoURL,
			TeamName:    u.TeamName,
			TotalPoints: 0,
		})
	}
	writeJSON(w, http.StatusOK, models.Leaderboard{Standings: standings})
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
