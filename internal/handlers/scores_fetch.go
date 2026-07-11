package handlers

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"unicode"

	"github.com/go-chi/chi/v5"
	"golang.org/x/text/unicode/norm"

	"github.com/andrewmartin/fantasy-league/internal/espn"
	"github.com/andrewmartin/fantasy-league/internal/models"
)

// FetchScores handles POST /api/admin/scores/fetch/{matchId}.
// Pulls the match's ESPN summary, maps player events to our drafted players,
// and returns a preview. Pass ?commit=true to apply it through the same
// scoring pipeline as manual entry. Idempotent on commit.
func (h *Handler) FetchScores(w http.ResponseWriter, r *http.Request) {
	matchID := chi.URLParam(r, "matchId")
	if matchID == "" {
		writeError(w, http.StatusBadRequest, "matchId is required")
		return
	}
	ctx := r.Context()

	var match models.Match
	found, err := h.DB.GetDoc(ctx, "matches", matchID, &match)
	if err != nil || !found {
		writeError(w, http.StatusNotFound, "match not found — create it with PUT /api/admin/match/{id} first")
		return
	}
	if strings.TrimSpace(match.ESPNEventID) == "" {
		writeError(w, http.StatusBadRequest, "match has no espnEventId — set it on the match first")
		return
	}

	sum, err := espn.FetchSummary(ctx, match.ESPNEventID)
	if err != nil {
		h.Log.Error("espn fetch", "matchId", matchID, "espnEventId", match.ESPNEventID, "err", err)
		writeError(w, http.StatusBadGateway, "could not fetch match from ESPN: "+err.Error())
		return
	}
	if !sum.Completed {
		writeError(w, http.StatusConflict, "match is not finished yet on ESPN")
		return
	}

	// Result → win/draw. Penalty shootouts count as draws here, so compare the
	// (regulation/ET) scores rather than ESPN's shootout-aware winner flag.
	draw := sum.Home.Score == sum.Away.Score
	homeWin := sum.Home.Score > sum.Away.Score
	awayWin := sum.Away.Score > sum.Home.Score

	var subs []statSubmission
	var preview []previewStat
	var unmatched []map[string]any

	processSide := func(side espn.TeamSide, country string, win, cleanSheet bool) error {
		cands, byESPN, err := h.loadCountryPlayers(ctx, country)
		if err != nil {
			return err
		}
		for _, a := range side.Athletes {
			if !a.Played {
				continue
			}
			// Exact ESPN-id match first (resolved once by cmd/espn-ids); fall
			// back to nation-scoped name matching only when no id is stored.
			pid, pos := "", ""
			if c, ok := byESPN[a.ESPNID]; ok && a.ESPNID != "" {
				pid, pos = c.ID, c.Position
			} else {
				pid, pos = matchAthlete(a.Name, cands)
			}
			if pid == "" {
				// Only flag unmatched athletes that actually earned/lost points —
				// a bench-warmer with no events isn't worth admin attention.
				if a.Goals > 0 || a.Assists > 0 || a.Yellow > 0 || a.Red > 0 {
					unmatched = append(unmatched, map[string]any{
						"name": a.Name, "team": country,
						"goals": a.Goals, "assists": a.Assists,
						"yellowCards": a.Yellow, "redCards": a.Red,
					})
				}
				continue
			}
			cs := cleanSheet && (pos == "GK" || pos == "DEF")
			ss := statSubmission{
				PlayerID: pid, Goals: a.Goals, Assists: a.Assists,
				YellowCards: a.Yellow, RedCards: a.Red,
				CleanSheet: cs, TeamWin: win, TeamDraw: draw,
			}
			subs = append(subs, ss)
			pts := models.CalculatePoints(models.PlayerMatchStats{
				Goals: ss.Goals, Assists: ss.Assists, CleanSheet: ss.CleanSheet,
				YellowCards: ss.YellowCards, RedCards: ss.RedCards,
				TeamWin: ss.TeamWin, TeamDraw: ss.TeamDraw,
			}, pos)
			preview = append(preview, previewStat{
				PlayerID: pid, Name: a.Name, Team: country, Position: pos,
				Goals: ss.Goals, Assists: ss.Assists, Yellow: ss.YellowCards, Red: ss.RedCards,
				CleanSheet: ss.CleanSheet, TeamWin: ss.TeamWin, TeamDraw: ss.TeamDraw, Points: pts,
			})
		}
		return nil
	}

	if err := processSide(sum.Home, match.HomeTeam, homeWin, sum.Away.Score == 0); err != nil {
		h.Log.Error("fetch scores: home side", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error mapping players")
		return
	}
	if err := processSide(sum.Away, match.AwayTeam, awayWin, sum.Home.Score == 0); err != nil {
		h.Log.Error("fetch scores: away side", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error mapping players")
		return
	}

	committed := false
	if r.URL.Query().Get("commit") == "true" {
		if err := h.applyMatchStats(ctx, matchID, subs); err != nil {
			h.Log.Error("fetch scores: commit", "matchId", matchID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error committing scores")
			return
		}
		committed = true
		h.Log.Info("scores fetched + committed", "matchId", matchID, "matched", len(subs), "unmatched", len(unmatched))
	}

	result := "draw"
	if homeWin {
		result = "home"
	} else if awayWin {
		result = "away"
	}
	sort.Slice(preview, func(i, j int) bool { return preview[i].Points > preview[j].Points })

	writeJSON(w, http.StatusOK, map[string]any{
		"matchId":     matchID,
		"espnEventId": match.ESPNEventID,
		"home":        map[string]any{"team": match.HomeTeam, "score": sum.Home.Score},
		"away":        map[string]any{"team": match.AwayTeam, "score": sum.Away.Score},
		"result":      result,
		"committed":   committed,
		"matched":     len(subs),
		"stats":       preview,
		"unmatched":   unmatched, // events ESPN reported for players we couldn't map — review these
	})
}

type previewStat struct {
	PlayerID   string `json:"playerId"`
	Name       string `json:"name"`
	Team       string `json:"team"`
	Position   string `json:"position"`
	Goals      int    `json:"goals"`
	Assists    int    `json:"assists"`
	Yellow     int    `json:"yellowCards"`
	Red        int    `json:"redCards"`
	CleanSheet bool   `json:"cleanSheet"`
	TeamWin    bool   `json:"teamWin"`
	TeamDraw   bool   `json:"teamDraw"`
	Points     int    `json:"points"`
}

// playerCand is a drafted-pool player we can match an ESPN athlete against.
type playerCand struct {
	ID       string
	Position string
	norm     string
	words    []string
}

// loadCountryPlayers returns one nation's players for name matching, plus an
// index by ESPN athlete id for exact lookup.
func (h *Handler) loadCountryPlayers(ctx context.Context, country string) ([]playerCand, map[string]playerCand, error) {
	docs, err := h.DB.FS.Collection("players").Where("country", "==", country).Documents(ctx).GetAll()
	if err != nil {
		return nil, nil, err
	}
	cands := make([]playerCand, 0, len(docs))
	byESPN := make(map[string]playerCand)
	for _, d := range docs {
		var p models.Player
		if d.DataTo(&p) != nil {
			continue
		}
		n := normalizePlayerName(p.Name)
		c := playerCand{ID: p.ID, Position: p.Position, norm: n, words: strings.Fields(n)}
		cands = append(cands, c)
		if p.ESPNAthleteID != "" {
			byESPN[p.ESPNAthleteID] = c
		}
	}
	return cands, byESPN, nil
}

// matchAthlete maps an ESPN athlete name to a player id within one nation's
// (small) candidate set. Exact match wins; fuzzy passes require a unique
// candidate so an ambiguous name is left unmatched rather than mis-credited.
func matchAthlete(espnName string, cands []playerCand) (id, position string) {
	n := normalizePlayerName(espnName)
	if n == "" {
		return "", ""
	}
	// Pass 1: exact normalized name.
	for _, c := range cands {
		if c.norm == n {
			return c.ID, c.Position
		}
	}
	words := strings.Fields(n)
	// Pass 2: every word of one name appears in the other (unique).
	var hits []playerCand
	for _, c := range cands {
		if subset(words, c.words) || subset(c.words, words) {
			hits = append(hits, c)
		}
	}
	if len(hits) == 1 {
		return hits[0].ID, hits[0].Position
	}
	// Pass 3: shared surname (last token), unique.
	if len(words) > 0 {
		last := words[len(words)-1]
		hits = hits[:0]
		for _, c := range cands {
			if len(c.words) > 0 && c.words[len(c.words)-1] == last {
				hits = append(hits, c)
			}
		}
		if len(hits) == 1 {
			return hits[0].ID, hits[0].Position
		}
	}
	return "", ""
}

// subset reports whether every word in a appears in b.
func subset(a, b []string) bool {
	if len(a) == 0 {
		return false
	}
	for _, w := range a {
		found := false
		for _, x := range b {
			if x == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// normalizePlayerName lowercases, strips accents/punctuation, and collapses
// spaces so ESPN names compare against our pool's spellings.
func normalizePlayerName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("-", " ", "'", " ", "’", " ", ".", " ").Replace(s)
	s = strings.NewReplacer("ø", "o", "đ", "d", "ð", "d", "ł", "l", "ß", "ss",
		"þ", "th", "ı", "i", "æ", "ae", "œ", "oe").Replace(s)
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
