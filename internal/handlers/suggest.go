package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/andrewmartin/fantasy-league/internal/middleware"
	"github.com/andrewmartin/fantasy-league/internal/models"
)

type suggestRequest struct {
	MyRoster         []suggestRosterPlayer `json:"myRoster"`
	AvailablePlayers []suggestPlayer       `json:"availablePlayers"`
}

type suggestRosterPlayer struct {
	Name     string `json:"name"`
	Position string `json:"position"`
}

type suggestPlayer struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position string `json:"position"`
	Rating   int    `json:"rating"`
	Country  string `json:"country"`
}

type SuggestResponse struct {
	Suggestions []string `json:"suggestions"`
	Reasoning   string   `json:"reasoning"`
}

// SuggestPick handles POST /api/draft/suggest — authenticated.
// Calls the Anthropic API to suggest picks; falls back to local logic if unavailable.
func (h *Handler) SuggestPick(w http.ResponseWriter, r *http.Request) {
	uid := middleware.UIDFromContext(r.Context())

	var req suggestRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		result, err := callAnthropic(apiKey, req.MyRoster, req.AvailablePlayers)
		if err == nil {
			h.Log.Info("suggest: anthropic success", "uid", uid)
			writeJSON(w, http.StatusOK, result)
			return
		}
		h.Log.Warn("suggest: anthropic failed, using local", "uid", uid, "err", err)
	}

	writeJSON(w, http.StatusOK, localSuggest(req.MyRoster, req.AvailablePlayers))
}

func callAnthropic(apiKey string, roster []suggestRosterPlayer, available []suggestPlayer) (*SuggestResponse, error) {
	rosterDesc := "none yet"
	if len(roster) > 0 {
		parts := make([]string, len(roster))
		for i, p := range roster {
			parts[i] = fmt.Sprintf("%s (%s)", p.Name, p.Position)
		}
		rosterDesc = strings.Join(parts, ", ")
	}

	top := make([]suggestPlayer, len(available))
	copy(top, available)
	sort.Slice(top, func(i, j int) bool { return top[i].Rating > top[j].Rating })
	if len(top) > 20 {
		top = top[:20]
	}
	topPlayerLines := make([]string, len(top))
	for i, p := range top {
		topPlayerLines[i] = fmt.Sprintf("- %s, %s, %s, rating %d", p.Name, p.Country, p.Position, p.Rating)
	}

	prompt := fmt.Sprintf(`You are advising a friend in a World Cup 2026 fantasy soccer draft.
Snake draft, 11 players per team (1 GK, 4 DEF, 4 MID, 2 FWD).
Current roster: %s
Top available players:
%s

Suggest exactly 3 players to draft next. Prioritise filling position needs first, then tournament form and rating.
Respond in exactly this format (nothing else):
1. [Player Name] — [one sentence reason]
2. [Player Name] — [one sentence reason]
3. [Player Name] — [one sentence reason]`,
		rosterDesc, strings.Join(topPlayerLines, "\n"))

	reqBody := map[string]interface{}{
		"model":      "claude-sonnet-4-6",
		"max_tokens": 300,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("content-type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic %d: %s", resp.StatusCode, body)
	}

	var anthropicResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return nil, err
	}
	if len(anthropicResp.Content) == 0 {
		return nil, fmt.Errorf("empty response from anthropic")
	}

	return parseSuggestResponse(anthropicResp.Content[0].Text), nil
}

func parseSuggestResponse(text string) *SuggestResponse {
	suggestions := []string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"1.", "2.", "3."} {
			if strings.HasPrefix(line, prefix) {
				rest := strings.TrimSpace(strings.TrimPrefix(line, prefix))
				// Extract player name before the dash separator
				if idx := strings.Index(rest, " — "); idx > 0 {
					suggestions = append(suggestions, strings.TrimSpace(rest[:idx]))
				} else if idx := strings.Index(rest, " - "); idx > 0 {
					suggestions = append(suggestions, strings.TrimSpace(rest[:idx]))
				} else {
					suggestions = append(suggestions, rest)
				}
			}
		}
	}
	return &SuggestResponse{Suggestions: suggestions, Reasoning: text}
}

func localSuggest(roster []suggestRosterPlayer, available []suggestPlayer) *SuggestResponse {
	posCounts := map[string]int{}
	for _, p := range roster {
		posCounts[p.Position]++
	}

	needed := map[string]bool{}
	for pos, limit := range models.PositionLimits {
		if posCounts[pos] < limit {
			needed[pos] = true
		}
	}

	candidates := make([]suggestPlayer, 0, len(available))
	for _, p := range available {
		if needed[p.Position] {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		candidates = available
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Rating > candidates[j].Rating
	})
	if len(candidates) > 3 {
		candidates = candidates[:3]
	}

	names := make([]string, len(candidates))
	for i, p := range candidates {
		names[i] = p.Name
	}
	return &SuggestResponse{
		Suggestions: names,
		Reasoning:   "Top picks by position need and rating (set ANTHROPIC_API_KEY for AI suggestions).",
	}
}
