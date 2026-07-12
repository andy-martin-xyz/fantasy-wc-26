package handlers

import (
	"context"
	"net/http"
	"sort"

	"github.com/andrewmartin/fantasy-league/internal/espn"
	"github.com/andrewmartin/fantasy-league/internal/models"
)

// wcTournamentDates is ESPN's dates value for the whole tournament window.
const wcTournamentDates = "20260611-20260719"

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
		dates = wcTournamentDates
	}
	ctx := r.Context()

	fixtures, err := espn.FetchFixtures(ctx, dates)
	if err != nil {
		h.Log.Error("import fixtures: espn", "err", err)
		writeError(w, http.StatusBadGateway, "could not fetch fixtures from ESPN: "+err.Error())
		return
	}

	res := h.upsertFixtures(ctx, fixtures, true)

	h.Log.Info("fixtures imported", "imported", res.Imported, "updated", res.Updated, "skipped", len(res.Skipped))
	writeJSON(w, http.StatusOK, map[string]any{
		"imported": res.Imported, "updated": res.Updated, "skipped": res.Skipped, "total": len(fixtures),
	})
}

// fixtureUpsertResult summarises one upsertFixtures pass.
type fixtureUpsertResult struct {
	Imported int                 // fixtures newly created
	Updated  int                 // existing fixtures refreshed (updateExisting only)
	Skipped  []map[string]string // fixtures with unrecognised (placeholder) team names
}

// upsertFixtures writes match docs for recognised fixtures — the one
// fixture→match path shared by the admin import and the cron sync.
// Placeholder team names (e.g. "Semifinal Winner") are skipped until ESPN
// resolves them. With updateExisting=false, fixtures already in Firestore are
// left untouched (cron sync); with true they are refreshed, preserving the
// scoring state of already-scored matches (admin re-import).
func (h *Handler) upsertFixtures(ctx context.Context, fixtures []espn.Fixture, updateExisting bool) fixtureUpsertResult {
	var res fixtureUpsertResult
	for _, f := range fixtures {
		home := models.NormalizeTeamName(f.HomeTeam)
		away := models.NormalizeTeamName(f.AwayTeam)
		if home == "" || away == "" {
			res.Skipped = append(res.Skipped, map[string]string{
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

		var existing models.Match
		found, err := h.DB.GetDoc(ctx, "matches", f.EventID, &existing)
		if err != nil {
			h.Log.Error("upsert fixtures: get match", "id", f.EventID, "err", err)
			continue
		}
		if found {
			if !updateExisting {
				continue // cron sync never touches known fixtures
			}
			// Preserve scoring state if this match was already processed.
			if existing.ScoringProcessed {
				match.ScoringProcessed = true
				match.Status = "complete"
				match.HomeScore = existing.HomeScore
				match.AwayScore = existing.AwayScore
			}
		}

		if err := h.DB.SetDoc(ctx, "matches", f.EventID, match); err != nil {
			h.Log.Error("upsert fixtures: set match", "id", f.EventID, "err", err)
			continue
		}
		if found {
			res.Updated++
		} else {
			res.Imported++
			h.Log.Info("upsert fixtures: imported", "id", f.EventID, "home", home, "away", away, "date", f.Date.Format("Jan 02"))
		}
	}
	return res
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
