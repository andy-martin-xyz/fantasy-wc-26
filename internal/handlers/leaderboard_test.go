package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andrewmartin/fantasy-league/internal/models"
)

// getTeamRequest builds a GET /api/team/{uid} request with the chi URL param set.
func getTeamRequest(uid string) *http.Request {
	req := httptest.NewRequest("GET", "/api/team/"+uid, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("uid", uid)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestGetTeam(t *testing.T) {
	dbClient := newEmulatorClient(t)
	ctx := context.Background()

	t.Run("roster not found", func(t *testing.T) {
		handler := newTestHandler(t, dbClient)

		w := httptest.NewRecorder()
		handler.GetTeam(w, getTeamRequest("nobody"))

		if w.Code != http.StatusNotFound {
			t.Errorf("GetTeam() status = %v, want %v, body=%s", w.Code, http.StatusNotFound, w.Body.String())
		}
	})

	t.Run("empty roster", func(t *testing.T) {
		handler := newTestHandler(t, dbClient)

		if err := handler.DB.SetDoc(ctx, "rosters", "user1", models.Roster{
			UserID: "user1", TeamName: "Empty FC", Players: []models.RosterPlayer{},
		}); err != nil {
			t.Fatalf("seed roster: %v", err)
		}

		w := httptest.NewRecorder()
		handler.GetTeam(w, getTeamRequest("user1"))

		if w.Code != http.StatusOK {
			t.Fatalf("GetTeam() status = %v, want %v, body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var resp teamResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(resp.Players) != 0 {
			t.Errorf("expected 0 players, got %d", len(resp.Players))
		}
	})

	t.Run("roster with stats and match enrichment", func(t *testing.T) {
		handler := newTestHandler(t, dbClient)

		kickoff := time.Date(2026, 6, 15, 18, 0, 0, 0, time.UTC)
		if err := handler.DB.SetDoc(ctx, "rosters", "user1", models.Roster{
			UserID: "user1", TeamName: "Test FC", TotalPoints: 7,
			Players: []models.RosterPlayer{
				{PlayerID: "p1", Name: "Player One", Country: "France", Position: "FWD"},
				{PlayerID: "p2", Name: "Player Two", Country: "Spain", Position: "GK"},
				{PlayerID: "ghost", Name: "Deleted Player", Country: "Norway", Position: "MID"},
			},
		}); err != nil {
			t.Fatalf("seed roster: %v", err)
		}
		if err := handler.DB.SetDoc(ctx, "players", "p1", models.Player{
			ID: "p1", Name: "Player One", Country: "France", Position: "FWD",
			ClubTeam: "PSG", TotalPoints: 7, Drafted: true, DraftedBy: "user1",
		}); err != nil {
			t.Fatalf("seed p1: %v", err)
		}
		if err := handler.DB.SetDoc(ctx, "players", "p2", models.Player{
			ID: "p2", Name: "Player Two", Country: "Spain", Position: "GK",
			Drafted: true, DraftedBy: "user1",
		}); err != nil {
			t.Fatalf("seed p2: %v", err)
		}
		if err := handler.DB.SetDoc(ctx, "matches", "m1", models.Match{
			ID: "m1", HomeTeam: "France", AwayTeam: "Germany", Date: kickoff,
		}); err != nil {
			t.Fatalf("seed match: %v", err)
		}
		if err := handler.DB.SetDoc(ctx, "playerMatchStats", "p1_m1", models.PlayerMatchStats{
			PlayerID: "p1", MatchID: "m1", Goals: 1, TeamWin: true, PointsAwarded: 7,
		}); err != nil {
			t.Fatalf("seed stats: %v", err)
		}

		w := httptest.NewRecorder()
		handler.GetTeam(w, getTeamRequest("user1"))

		if w.Code != http.StatusOK {
			t.Fatalf("GetTeam() status = %v, want %v, body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var resp teamResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(resp.Players) != 3 {
			t.Fatalf("expected 3 players, got %d", len(resp.Players))
		}

		byID := map[string]playerWithPoints{}
		for _, p := range resp.Players {
			byID[p.PlayerID] = p
		}

		p1 := byID["p1"]
		if p1.TotalPoints != 7 || p1.ClubTeam != "PSG" {
			t.Errorf("p1 enrichment wrong: totalPoints=%d clubTeam=%q", p1.TotalPoints, p1.ClubTeam)
		}
		if len(p1.MatchStats) != 1 {
			t.Fatalf("expected 1 stat row for p1, got %d", len(p1.MatchStats))
		}
		row := p1.MatchStats[0]
		if row.Opponent != "Germany" {
			t.Errorf("expected opponent Germany (from France's perspective), got %q", row.Opponent)
		}
		if !row.Date.Equal(kickoff) {
			t.Errorf("expected match date %v, got %v", kickoff, row.Date)
		}
		if row.PointsAwarded != 7 || row.Goals != 1 {
			t.Errorf("stat row wrong: %+v", row)
		}

		if p2 := byID["p2"]; len(p2.MatchStats) != 0 {
			t.Errorf("expected no stat rows for p2, got %d", len(p2.MatchStats))
		}
		// A roster entry whose player doc is gone still appears, just unenriched.
		if _, ok := byID["ghost"]; !ok {
			t.Errorf("expected ghost roster entry to survive missing player doc")
		}
	})
}
