package handlers

import (
	"net/http"
	"sort"

	"github.com/andrewmartin/fantasy-league/internal/espn"
	"github.com/andrewmartin/fantasy-league/internal/models"
)

// ImportFixtures handles POST /api/admin/fixtures/import.
// Pulls the World Cup schedule from ESPN and upserts a match doc per fixture
// (keyed by ESPN event id, with espnEventId set so scores can be auto-fetched).
// Idempotent: re-running preserves the scoring state of already-scored matches.
type importFixturesRequest struct {
	Dates string `json:"dates"` // ESPN dates value; default = whole tournament window
}

func (h *Handler) ImportFixtures(w http.ResponseWriter, r *http.Request) {
	var req importFixturesRequest
	_ = decode(r, &req) // body optional
	dates := req.Dates
	if dates == "" {
		dates = "20260611-20260719"
	}
	ctx := r.Context()

	fixtures, err := espn.FetchFixtures(ctx, dates)
	if err != nil {
		h.Log.Error("import fixtures: espn", "err", err)
		writeError(w, http.StatusBadGateway, "could not fetch fixtures from ESPN: "+err.Error())
		return
	}

	imported, updated := 0, 0
	var skipped []map[string]string
	for _, f := range fixtures {
		home := models.NormalizeTeamName(f.HomeTeam)
		away := models.NormalizeTeamName(f.AwayTeam)
		if home == "" || away == "" {
			skipped = append(skipped, map[string]string{
				"home": f.HomeTeam, "away": f.AwayTeam, "reason": "unrecognised team name",
			})
			continue
		}

		match := models.Match{
			ID:          f.EventID,
			HomeTeam:    home,
			AwayTeam:    away,
			Date:        f.Date,
			Status:      statusFromState(f.State),
			ESPNEventID: f.EventID,
		}

		// Preserve scoring state if this match was already processed.
		var existing models.Match
		found, err := h.DB.GetDoc(ctx, "matches", f.EventID, &existing)
		if err != nil {
			h.Log.Error("import fixtures: get match", "id", f.EventID, "err", err)
			continue
		}
		if found {
			if existing.ScoringProcessed {
				match.ScoringProcessed = true
				match.Status = "complete"
				match.HomeScore = existing.HomeScore
				match.AwayScore = existing.AwayScore
			}
			updated++
		} else {
			imported++
		}

		if err := h.DB.SetDoc(ctx, "matches", f.EventID, match); err != nil {
			h.Log.Error("import fixtures: set match", "id", f.EventID, "err", err)
			continue
		}
	}

	h.Log.Info("fixtures imported", "imported", imported, "updated", updated, "skipped", len(skipped))
	writeJSON(w, http.StatusOK, map[string]any{
		"imported": imported, "updated": updated, "skipped": skipped, "total": len(fixtures),
	})
}

// GetMatches handles GET /api/admin/matches — all matches, soonest first.
func (h *Handler) GetMatches(w http.ResponseWriter, r *http.Request) {
	docs, err := h.DB.FS.Collection("matches").Documents(r.Context()).GetAll()
	if err != nil {
		h.Log.Error("get matches", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	matches := make([]models.Match, 0, len(docs))
	for _, d := range docs {
		var m models.Match
		if d.DataTo(&m) == nil {
			matches = append(matches, m)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Date.Before(matches[j].Date) })
	writeJSON(w, http.StatusOK, matches)
}

func statusFromState(state string) string {
	switch state {
	case "in":
		return "live"
	case "post":
		return "complete"
	default:
		return "upcoming"
	}
}
