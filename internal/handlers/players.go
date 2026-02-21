package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/andrewmartin/fantasy-league/internal/models"
)

// ImportPlayers handles POST /api/admin/players/import.
// Accepts a JSON array of player objects.
// Idempotent: upserts by name+country (overwrites existing doc).
type importPlayersRequest struct {
	Players []importPlayer `json:"players"`
}

type importPlayer struct {
	Name     string `json:"name"`
	Country  string `json:"country"`
	Position string `json:"position"`
	ClubTeam string `json:"clubTeam"`
	Rating   int    `json:"rating"`
	ImageURL string `json:"imageURL"`
}

func (h *Handler) ImportPlayers(w http.ResponseWriter, r *http.Request) {
	var req importPlayersRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.Players) == 0 {
		writeError(w, http.StatusBadRequest, "players array is empty")
		return
	}

	var errs []string
	var upserted int

	for i, p := range req.Players {
		p.Name = strings.TrimSpace(p.Name)
		p.Country = strings.TrimSpace(p.Country)
		p.Position = strings.ToUpper(strings.TrimSpace(p.Position))

		if p.Name == "" || p.Country == "" {
			errs = append(errs, fmt.Sprintf("index %d: name and country are required", i))
			continue
		}
		if !models.ValidPositions[p.Position] {
			errs = append(errs, fmt.Sprintf("index %d (%s): invalid position %q (must be GK/DEF/MID/FWD)", i, p.Name, p.Position))
			continue
		}

		// Stable document ID: slugified name+country.
		docID := playerDocID(p.Name, p.Country)

		player := models.Player{
			ID:       docID,
			Name:     p.Name,
			Country:  p.Country,
			Position: p.Position,
			ClubTeam: p.ClubTeam,
			Rating:   p.Rating,
			ImageURL: p.ImageURL,
			// Preserve draft state if player already exists.
		}

		// Upsert: SetDoc overwrites, but we want to preserve drafted/draftedBy/totalPoints.
		// Check if doc exists first; if so, only update non-draft fields.
		var existing models.Player
		found, err := h.DB.GetDoc(r.Context(), "players", docID, &existing)
		if err != nil {
			h.Log.Error("import: get player", "id", docID, "err", err)
			errs = append(errs, fmt.Sprintf("index %d (%s): internal error", i, p.Name))
			continue
		}
		if found {
			// Preserve draft state.
			player.Drafted = existing.Drafted
			player.DraftedBy = existing.DraftedBy
			player.TotalPoints = existing.TotalPoints
		}

		if err := h.DB.SetDoc(r.Context(), "players", docID, player); err != nil {
			h.Log.Error("import: set player", "id", docID, "err", err)
			errs = append(errs, fmt.Sprintf("index %d (%s): write failed", i, p.Name))
			continue
		}
		upserted++
	}

	resp := map[string]any{
		"upserted": upserted,
		"errors":   errs,
	}
	if len(errs) > 0 {
		writeJSON(w, http.StatusMultiStatus, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetPlayers handles GET /api/players — returns all undrafted players.
func (h *Handler) GetPlayers(w http.ResponseWriter, r *http.Request) {
	docs, err := h.DB.FS.Collection("players").Documents(r.Context()).GetAll()
	if err != nil {
		h.Log.Error("get players", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	players := make([]models.Player, 0, len(docs))
	for _, d := range docs {
		var p models.Player
		if err := d.DataTo(&p); err != nil {
			continue
		}
		players = append(players, p)
	}
	writeJSON(w, http.StatusOK, players)
}

// playerDocID creates a stable Firestore document ID from name + country.
// e.g. "Kylian Mbappe" + "France" → "kylian-mbappe_france"
func playerDocID(name, country string) string {
	slug := func(s string) string {
		s = strings.ToLower(s)
		var out []rune
		for _, r := range s {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				out = append(out, r)
			} else {
				out = append(out, '-')
			}
		}
		// Collapse consecutive dashes.
		result := strings.Join(strings.Fields(string(out)), "-")
		return strings.Trim(result, "-")
	}
	return slug(name) + "_" + slug(country)
}
