package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/andrewmartin/fantasy-league/internal/models"
)

// TestProcessScores exercises the whole manual-entry scoring pipeline against
// the emulator: writeStats → recalculateTotals → match marked complete. This
// is the same shared pipeline the ESPN fetch and cron paths call, so it
// guards all three.
func TestProcessScores(t *testing.T) {
	dbClient := newEmulatorClient(t)
	ctx := context.Background()

	handler := newTestHandler(t, dbClient)

	// Two users: user1's FWD scores twice in a win, user2's GK concedes on
	// the losing side and earns nothing.
	seed := []struct {
		collection, id string
		data           interface{}
	}{
		{"users", "user1", models.User{UID: "user1", DisplayName: "One", TeamName: "One FC"}},
		{"users", "user2", models.User{UID: "user2", DisplayName: "Two", TeamName: "Two FC"}},
		{"players", "fwd1", models.Player{ID: "fwd1", Name: "Striker", Country: "France", Position: "FWD", Drafted: true, DraftedBy: "user1"}},
		{"players", "gk1", models.Player{ID: "gk1", Name: "Keeper", Country: "Germany", Position: "GK", Drafted: true, DraftedBy: "user2"}},
		{"rosters", "user1", models.Roster{UserID: "user1", TeamName: "One FC", Players: []models.RosterPlayer{{PlayerID: "fwd1", Name: "Striker", Country: "France", Position: "FWD"}}}},
		{"rosters", "user2", models.Roster{UserID: "user2", TeamName: "Two FC", Players: []models.RosterPlayer{{PlayerID: "gk1", Name: "Keeper", Country: "Germany", Position: "GK"}}}},
		{"matches", "m1", models.Match{ID: "m1", HomeTeam: "France", AwayTeam: "Germany"}},
	}
	for _, s := range seed {
		if err := handler.DB.SetDoc(ctx, s.collection, s.id, s.data); err != nil {
			t.Fatalf("seed %s/%s: %v", s.collection, s.id, err)
		}
	}

	// France 2-0 Germany: striker scores twice in a win (2*5 + 2 = 12 pts),
	// keeper concedes twice on the losing side (0 pts).
	body, _ := json.Marshal(processScoresRequest{
		MatchID: "m1",
		Stats: []statSubmission{
			{PlayerID: "fwd1", Goals: 2, TeamWin: true},
			{PlayerID: "gk1"},
			{PlayerID: "does-not-exist", Goals: 9}, // unknown players are skipped, not fatal
		},
	})
	req := httptest.NewRequest("POST", "/api/admin/scores/process", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ProcessScores(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("ProcessScores() status = %v, body=%s", w.Code, w.Body.String())
	}

	// Stats doc written with computed points.
	var ps models.PlayerMatchStats
	found, err := handler.DB.GetDoc(ctx, "playerMatchStats", "fwd1_m1", &ps)
	if err != nil || !found {
		t.Fatalf("expected fwd1_m1 stats doc, found=%v err=%v", found, err)
	}
	if ps.PointsAwarded != 12 {
		t.Errorf("fwd1 points = %d, want 12 (2 goals x5 + win x2)", ps.PointsAwarded)
	}

	// Player total recalculated.
	var fwd models.Player
	if _, err := handler.DB.GetDoc(ctx, "players", "fwd1", &fwd); err != nil {
		t.Fatalf("get fwd1: %v", err)
	}
	if fwd.TotalPoints != 12 {
		t.Errorf("fwd1 totalPoints = %d, want 12", fwd.TotalPoints)
	}

	// Roster totals recalculated.
	var r1, r2 models.Roster
	if _, err := handler.DB.GetDoc(ctx, "rosters", "user1", &r1); err != nil {
		t.Fatalf("get roster user1: %v", err)
	}
	if _, err := handler.DB.GetDoc(ctx, "rosters", "user2", &r2); err != nil {
		t.Fatalf("get roster user2: %v", err)
	}
	if r1.TotalPoints != 12 || r2.TotalPoints != 0 {
		t.Errorf("roster totals = %d/%d, want 12/0", r1.TotalPoints, r2.TotalPoints)
	}

	// Leaderboard rebuilt, sorted, joined with user names.
	var lb models.Leaderboard
	found, err = handler.DB.GetDoc(ctx, "leaderboard", "current", &lb)
	if err != nil || !found {
		t.Fatalf("expected leaderboard/current, found=%v err=%v", found, err)
	}
	if len(lb.Standings) != 2 || lb.Standings[0].UserID != "user1" || lb.Standings[0].TotalPoints != 12 {
		t.Errorf("leaderboard wrong: %+v", lb.Standings)
	}
	if lb.Standings[0].DisplayName != "One" {
		t.Errorf("leaderboard user join wrong: %+v", lb.Standings[0])
	}

	// Match marked complete.
	var m models.Match
	if _, err := handler.DB.GetDoc(ctx, "matches", "m1", &m); err != nil {
		t.Fatalf("get match: %v", err)
	}
	if !m.ScoringProcessed || m.Status != "complete" {
		t.Errorf("match not marked scored: processed=%v status=%q", m.ScoringProcessed, m.Status)
	}

	// Idempotency: re-running the same match overwrites, not doubles.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/admin/scores/process", bytes.NewBuffer(body))
	req2.Header.Set("Content-Type", "application/json")
	handler.ProcessScores(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("re-run status = %v", w2.Code)
	}
	if _, err := handler.DB.GetDoc(ctx, "players", "fwd1", &fwd); err != nil {
		t.Fatalf("get fwd1 after re-run: %v", err)
	}
	if fwd.TotalPoints != 12 {
		t.Errorf("re-run doubled points: totalPoints = %d, want 12", fwd.TotalPoints)
	}
}
