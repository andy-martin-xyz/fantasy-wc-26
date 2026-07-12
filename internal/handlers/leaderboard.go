package handlers

import (
	"net/http"
	"sort"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/go-chi/chi/v5"
	"github.com/andrewmartin/fantasy-league/internal/models"
)

// GetTeams handles GET /api/teams — public, no auth.
// Returns all WC 2026 qualified nations with flag and confederation.
func (h *Handler) GetTeams(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, models.WC2026Teams)
}

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

// matchStatRow is one match's stats for a player, enriched with match metadata
// (date, opponent) so the frontend can display a full per-game breakdown.
type matchStatRow struct {
	MatchID       string    `json:"matchId"`
	Date          time.Time `json:"date"`
	Opponent      string    `json:"opponent"`
	Goals         int       `json:"goals"`
	Assists       int       `json:"assists"`
	CleanSheet    bool      `json:"cleanSheet"`
	YellowCards   int       `json:"yellowCards"`
	RedCards      int       `json:"redCards"`
	TeamWin       bool      `json:"teamWin"`
	TeamDraw      bool      `json:"teamDraw"`
	PointsAwarded int       `json:"pointsAwarded"`
}

type playerWithPoints struct {
	models.RosterPlayer
	TotalPoints int            `json:"totalPoints"`
	ClubTeam    string         `json:"clubTeam"`
	MatchStats  []matchStatRow `json:"matchStats"`
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

	// One batched read for the roster's player docs (instead of one GetDoc
	// per player).
	playerRefs := make([]*firestore.DocumentRef, 0, len(roster.Players))
	for _, rp := range roster.Players {
		playerRefs = append(playerRefs, h.DB.FS.Collection("players").Doc(rp.PlayerID))
	}
	players := make(map[string]models.Player, len(playerRefs))
	if len(playerRefs) > 0 {
		snaps, err := h.DB.FS.GetAll(r.Context(), playerRefs)
		if err != nil {
			h.Log.Error("get team: batch get players", "uid", uid, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for _, snap := range snaps {
			if !snap.Exists() {
				continue
			}
			var p models.Player
			if snap.DataTo(&p) == nil {
				players[snap.Ref.ID] = p
			}
		}
	}

	// One stats query for the whole roster (Firestore `in` allows up to 30
	// values; rosters hold 11) instead of one query per player.
	statsByPlayer := make(map[string][]models.PlayerMatchStats)
	matchIDs := make(map[string]bool)
	if len(roster.Players) > 0 {
		ids := make([]string, 0, len(roster.Players))
		for _, rp := range roster.Players {
			ids = append(ids, rp.PlayerID)
		}
		statDocs, err := h.DB.FS.Collection("playerMatchStats").
			Where("playerId", "in", ids).
			Documents(r.Context()).GetAll()
		if err != nil {
			h.Log.Warn("get team: query stats", "uid", uid, "err", err)
			statDocs = nil
		}
		for _, sd := range statDocs {
			var ps models.PlayerMatchStats
			if sd.DataTo(&ps) != nil {
				continue
			}
			statsByPlayer[ps.PlayerID] = append(statsByPlayer[ps.PlayerID], ps)
			matchIDs[ps.MatchID] = true
		}
	}

	// Batch-read only the matches referenced by those stats (for opponent and
	// date), instead of scanning the whole matches collection.
	matchByID := make(map[string]models.Match, len(matchIDs))
	if len(matchIDs) > 0 {
		refs := make([]*firestore.DocumentRef, 0, len(matchIDs))
		for id := range matchIDs {
			refs = append(refs, h.DB.FS.Collection("matches").Doc(id))
		}
		snaps, err := h.DB.FS.GetAll(r.Context(), refs)
		if err != nil {
			h.Log.Warn("get team: batch get matches", "uid", uid, "err", err)
			snaps = nil
		}
		for _, snap := range snaps {
			if !snap.Exists() {
				continue
			}
			var m models.Match
			if snap.DataTo(&m) == nil {
				matchByID[snap.Ref.ID] = m
			}
		}
	}

	// Enrich each roster player with totalPoints and per-match breakdown.
	enriched := make([]playerWithPoints, 0, len(roster.Players))
	for _, rp := range roster.Players {
		p, found := players[rp.PlayerID]
		if !found {
			enriched = append(enriched, playerWithPoints{RosterPlayer: rp, MatchStats: []matchStatRow{}})
			continue
		}

		stats := statsByPlayer[rp.PlayerID]
		rows := make([]matchStatRow, 0, len(stats))
		for _, ps := range stats {
			row := matchStatRow{
				MatchID:       ps.MatchID,
				Goals:         ps.Goals,
				Assists:       ps.Assists,
				CleanSheet:    ps.CleanSheet,
				YellowCards:   ps.YellowCards,
				RedCards:      ps.RedCards,
				TeamWin:       ps.TeamWin,
				TeamDraw:      ps.TeamDraw,
				PointsAwarded: ps.PointsAwarded,
			}
			if m, ok := matchByID[ps.MatchID]; ok {
				row.Date = m.Date
				// Show the opponent from this player's perspective.
				if m.HomeTeam == rp.Country {
					row.Opponent = m.AwayTeam
				} else {
					row.Opponent = m.HomeTeam
				}
			}
			rows = append(rows, row)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Date.Before(rows[j].Date) })

		enriched = append(enriched, playerWithPoints{
			RosterPlayer: rp,
			TotalPoints:  p.TotalPoints,
			ClubTeam:     p.ClubTeam,
			MatchStats:   rows,
		})
	}

	writeJSON(w, http.StatusOK, teamResponse{
		UserID:      roster.UserID,
		TeamName:    roster.TeamName,
		TotalPoints: roster.TotalPoints,
		Players:     enriched,
	})
}
