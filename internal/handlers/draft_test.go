package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/andrewmartin/fantasy-league/internal/db"
	"github.com/andrewmartin/fantasy-league/internal/middleware"
	"github.com/andrewmartin/fantasy-league/internal/models"
)

// These tests exercise the draft handlers against a real Firestore emulator
// rather than a mock. Handler.DB is a concrete *db.Client wrapping a real
// *firestore.Client, and several handlers (SubmitPick, UndoLastPick,
// ResetDraft) use transactions, batches, and subcollection queries directly
// via h.DB.FS — none of that is reasonably fakeable behind a mock.
//
// Start the emulator with `firebase emulators:start --only firestore` (or
// `--only firestore,auth`) before running these tests. Without it, they
// skip cleanly rather than fail, so `go test ./...` is never red on a
// machine that doesn't have the emulator running.

const testProjectID = "fantasy-league-test"

// newEmulatorClient connects to the Firestore emulator, skipping the test if
// FIRESTORE_EMULATOR_HOST isn't set.
func newEmulatorClient(t *testing.T) *db.Client {
	t.Helper()
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set; run `firebase emulators:start --only firestore` to exercise this test")
	}
	os.Setenv("FIREBASE_PROJECT_ID", testProjectID)
	c, err := db.New(context.Background())
	if err != nil {
		t.Fatalf("connect to firestore emulator: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// clearEmulator wipes all documents so each subtest starts from a clean slate.
func clearEmulator(t *testing.T) {
	t.Helper()
	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	url := fmt.Sprintf("http://%s/emulator/v1/projects/%s/databases/(default)/documents", host, testProjectID)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatalf("build clear-emulator request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("clear emulator data: %v", err)
	}
	resp.Body.Close()
}

// newTestHandler clears the emulator and returns a Handler backed by dbClient.
func newTestHandler(t *testing.T, dbClient *db.Client) *Handler {
	t.Helper()
	clearEmulator(t)
	return &Handler{
		DB:  dbClient,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestGetDraftStatus(t *testing.T) {
	dbClient := newEmulatorClient(t)
	ctx := context.Background()

	tests := []struct {
		name           string
		configExists   bool
		configData     models.DraftConfig
		expectedStatus int
	}{
		{
			name:           "no config exists",
			configExists:   false,
			expectedStatus: http.StatusOK,
		},
		{
			name:         "config exists with pending status",
			configExists: true,
			configData:   models.DraftConfig{Status: "pending"},
			expectedStatus: http.StatusOK,
		},
		{
			name:         "config exists with active status",
			configExists: true,
			configData:   models.DraftConfig{Status: "active"},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newTestHandler(t, dbClient)

			if tt.configExists {
				if err := handler.DB.SetDoc(ctx, "draft", "config", tt.configData); err != nil {
					t.Fatalf("seed draft config: %v", err)
				}
			}

			req := httptest.NewRequest("GET", "/api/draft/status", nil)
			w := httptest.NewRecorder()

			handler.GetDraftStatus(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("GetDraftStatus() status = %v, want %v, body=%s", w.Code, tt.expectedStatus, w.Body.String())
			}
		})
	}
}

func TestSetDraftOrder(t *testing.T) {
	dbClient := newEmulatorClient(t)
	ctx := context.Background()

	tests := []struct {
		name           string
		requestBody    interface{}
		usersExist     bool
		expectedStatus int
	}{
		{
			name: "valid order provided",
			requestBody: setOrderRequest{
				Order: []string{"user1", "user2", "user3"},
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "randomize true with users",
			requestBody:    setOrderRequest{Randomize: true},
			usersExist:     true,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "randomize true without users",
			requestBody:    setOrderRequest{Randomize: true},
			expectedStatus: http.StatusOK, // still succeeds with an empty order
		},
		{
			name: "empty order with randomize false",
			requestBody: setOrderRequest{
				Order:     []string{},
				Randomize: false,
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newTestHandler(t, dbClient)

			if tt.usersExist {
				if err := handler.DB.SetDoc(ctx, "users", "user1", models.User{UID: "user1"}); err != nil {
					t.Fatalf("seed user1: %v", err)
				}
				if err := handler.DB.SetDoc(ctx, "users", "user2", models.User{UID: "user2"}); err != nil {
					t.Fatalf("seed user2: %v", err)
				}
			}

			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest("POST", "/api/admin/draft/set-order", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.SetDraftOrder(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("SetDraftOrder() status = %v, want %v, body=%s", w.Code, tt.expectedStatus, w.Body.String())
			}
		})
	}
}

func TestStartDraft(t *testing.T) {
	dbClient := newEmulatorClient(t)
	ctx := context.Background()

	tests := []struct {
		name           string
		configExists   bool
		configData     models.DraftConfig
		expectedStatus int
	}{
		{
			name:           "no config exists",
			configExists:   false,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "config exists but empty pick order",
			configExists:   true,
			configData:     models.DraftConfig{PickOrder: []string{}},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "config exists with valid pick order",
			configExists:   true,
			configData:     models.DraftConfig{PickOrder: []string{"user1", "user2"}},
			expectedStatus: http.StatusOK,
		},
		{
			name: "config exists with complete status",
			configExists: true,
			configData: models.DraftConfig{
				PickOrder: []string{"user1", "user2"},
				Status:    "complete",
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newTestHandler(t, dbClient)

			if tt.configExists {
				if err := handler.DB.SetDoc(ctx, "draft", "config", tt.configData); err != nil {
					t.Fatalf("seed draft config: %v", err)
				}
			}

			req := httptest.NewRequest("POST", "/api/admin/draft/start", nil)
			w := httptest.NewRecorder()

			handler.StartDraft(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("StartDraft() status = %v, want %v, body=%s", w.Code, tt.expectedStatus, w.Body.String())
			}
		})
	}
}

func TestSetDraftStatus(t *testing.T) {
	dbClient := newEmulatorClient(t)
	ctx := context.Background()

	tests := []struct {
		name           string
		currentStatus  string
		newStatus      string // "paused" -> PauseDraft, anything else -> ResumeDraft
		expectedStatus int
	}{
		{
			name:           "valid status change",
			currentStatus:  "active",
			newStatus:      "paused",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid status change",
			currentStatus:  "paused",
			newStatus:      "paused",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "valid resume",
			currentStatus:  "paused",
			newStatus:      "active",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newTestHandler(t, dbClient)

			if err := handler.DB.SetDoc(ctx, "draft", "config", models.DraftConfig{Status: tt.currentStatus}); err != nil {
				t.Fatalf("seed draft config: %v", err)
			}

			var req *http.Request
			if tt.newStatus == "paused" {
				req = httptest.NewRequest("POST", "/api/admin/draft/pause", nil)
			} else {
				req = httptest.NewRequest("POST", "/api/admin/draft/resume", nil)
			}
			w := httptest.NewRecorder()

			if tt.newStatus == "paused" {
				handler.PauseDraft(w, req)
			} else {
				handler.ResumeDraft(w, req)
			}

			if w.Code != tt.expectedStatus {
				t.Errorf("setDraftStatus() status = %v, want %v, body=%s", w.Code, tt.expectedStatus, w.Body.String())
			}
		})
	}
}

func TestSubmitPick(t *testing.T) {
	dbClient := newEmulatorClient(t)
	ctx := context.Background()

	tests := []struct {
		name           string
		uid            string
		playerID       string
		seedPlayer     bool
		playerDrafted  bool
		playerPosition string
		seedRoster     bool
		rosterPlayers  []models.RosterPlayer
		seedConfig     bool
		draftConfig    models.DraftConfig
		expectedStatus int
	}{
		{
			name:           "valid pick",
			uid:            "user1",
			playerID:       "player1",
			seedPlayer:     true,
			playerDrafted:  false,
			playerPosition: "FWD",
			seedRoster:     true,
			rosterPlayers:  []models.RosterPlayer{},
			seedConfig:     true,
			draftConfig: models.DraftConfig{
				Status:               "active",
				CurrentPickIndex:     0,
				PickOrder:            []string{"user1", "user2"},
				PickTimeLimitSeconds: 90,
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid player ID",
			uid:            "user1",
			playerID:       "",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "player already drafted",
			uid:            "user1",
			playerID:       "player1",
			seedPlayer:     true,
			playerDrafted:  true,
			playerPosition: "FWD",
			seedConfig:     true,
			draftConfig: models.DraftConfig{
				Status:               "active",
				CurrentPickIndex:     0,
				PickOrder:            []string{"user1", "user2"},
				PickTimeLimitSeconds: 90,
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "not user's turn",
			uid:            "user2",
			playerID:       "player1",
			seedPlayer:     true,
			playerDrafted:  false,
			playerPosition: "FWD",
			seedConfig:     true,
			draftConfig: models.DraftConfig{
				Status:               "active",
				CurrentPickIndex:     0,
				PickOrder:            []string{"user1", "user2"},
				PickTimeLimitSeconds: 90,
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "position limit exceeded",
			uid:            "user1",
			playerID:       "player1",
			seedPlayer:     true,
			playerDrafted:  false,
			playerPosition: "FWD",
			seedRoster:     true,
			rosterPlayers: []models.RosterPlayer{
				{PlayerID: "player2", Position: "FWD"},
				{PlayerID: "player3", Position: "FWD"},
			},
			seedConfig: true,
			draftConfig: models.DraftConfig{
				Status:               "active",
				CurrentPickIndex:     0,
				PickOrder:            []string{"user1", "user2"},
				PickTimeLimitSeconds: 90,
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newTestHandler(t, dbClient)

			if tt.seedConfig {
				if err := handler.DB.SetDoc(ctx, "draft", "config", tt.draftConfig); err != nil {
					t.Fatalf("seed draft config: %v", err)
				}
			}
			if tt.seedPlayer {
				if err := handler.DB.SetDoc(ctx, "players", "player1", models.Player{
					ID:       "player1",
					Name:     "Player One",
					Position: tt.playerPosition,
					Drafted:  tt.playerDrafted,
				}); err != nil {
					t.Fatalf("seed player1: %v", err)
				}
			}
			if tt.seedRoster {
				if err := handler.DB.SetDoc(ctx, "rosters", tt.uid, models.Roster{
					UserID:  tt.uid,
					Players: tt.rosterPlayers,
				}); err != nil {
					t.Fatalf("seed roster: %v", err)
				}
			}

			body, _ := json.Marshal(pickRequest{PlayerID: tt.playerID})
			req := httptest.NewRequest("POST", "/api/draft/pick", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			reqCtx := context.WithValue(req.Context(), middleware.CtxUID, tt.uid)
			req = req.WithContext(reqCtx)

			w := httptest.NewRecorder()
			handler.SubmitPick(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("SubmitPick() status = %v, want %v, body=%s", w.Code, tt.expectedStatus, w.Body.String())
			}

			if tt.name == "valid pick" && w.Code == http.StatusOK {
				var player models.Player
				found, err := handler.DB.GetDoc(ctx, "players", "player1", &player)
				if err != nil || !found {
					t.Fatalf("expected player1 to exist after pick, found=%v err=%v", found, err)
				}
				if !player.Drafted || player.DraftedBy != "user1" {
					t.Errorf("expected player1 drafted by user1, got drafted=%v draftedBy=%q", player.Drafted, player.DraftedBy)
				}

				var roster models.Roster
				found, err = handler.DB.GetDoc(ctx, "rosters", "user1", &roster)
				if err != nil || !found || len(roster.Players) != 1 || roster.Players[0].PlayerID != "player1" {
					t.Errorf("expected user1's roster to contain player1, got %+v (found=%v err=%v)", roster, found, err)
				}

				var cfg models.DraftConfig
				found, err = handler.DB.GetDoc(ctx, "draft", "config", &cfg)
				if err != nil || !found || cfg.CurrentPickIndex != 1 {
					t.Errorf("expected currentPickIndex=1 after pick, got %+v (found=%v err=%v)", cfg, found, err)
				}
			}
		})
	}
}

func TestUndoLastPick(t *testing.T) {
	dbClient := newEmulatorClient(t)
	ctx := context.Background()

	tests := []struct {
		name           string
		pickExists     bool
		expectedStatus int
	}{
		{
			name:           "pick exists",
			pickExists:     true,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "no picks to undo",
			pickExists:     false,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newTestHandler(t, dbClient)

			if tt.pickExists {
				if err := handler.DB.SetDoc(ctx, "draft", "config", models.DraftConfig{
					Status:               "active",
					CurrentPickIndex:     1,
					PickOrder:            []string{"user1", "user2"},
					PickTimeLimitSeconds: 90,
				}); err != nil {
					t.Fatalf("seed draft config: %v", err)
				}
				if err := handler.DB.SetDoc(ctx, "players", "player1", models.Player{
					ID:        "player1",
					Name:      "Player One",
					Position:  "FWD",
					Drafted:   true,
					DraftedBy: "user1",
				}); err != nil {
					t.Fatalf("seed player1: %v", err)
				}
				if err := handler.DB.SetDoc(ctx, "rosters", "user1", models.Roster{
					UserID: "user1",
					Players: []models.RosterPlayer{
						{PlayerID: "player1", Name: "Player One", Position: "FWD"},
					},
				}); err != nil {
					t.Fatalf("seed roster: %v", err)
				}
				pick := models.DraftPick{
					UserID:     "user1",
					PlayerID:   "player1",
					PlayerName: "Player One",
					Round:      1,
					PickNumber: 1,
				}
				if _, err := handler.DB.FS.Collection("draft").Doc("picks").Collection("items").Doc("1").Set(ctx, pick); err != nil {
					t.Fatalf("seed pick: %v", err)
				}
			}

			req := httptest.NewRequest("POST", "/api/admin/draft/undo-pick", nil)
			w := httptest.NewRecorder()

			handler.UndoLastPick(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("UndoLastPick() status = %v, want %v, body=%s", w.Code, tt.expectedStatus, w.Body.String())
			}

			if tt.pickExists && w.Code == http.StatusOK {
				var player models.Player
				found, err := handler.DB.GetDoc(ctx, "players", "player1", &player)
				if err != nil || !found || player.Drafted {
					t.Errorf("expected player1 reset to undrafted, got %+v (found=%v err=%v)", player, found, err)
				}

				var roster models.Roster
				found, err = handler.DB.GetDoc(ctx, "rosters", "user1", &roster)
				if err != nil || !found || len(roster.Players) != 0 {
					t.Errorf("expected user1's roster to no longer contain player1, got %+v (found=%v err=%v)", roster, found, err)
				}
			}
		})
	}
}

func TestResetDraft(t *testing.T) {
	dbClient := newEmulatorClient(t)
	ctx := context.Background()

	tests := []struct {
		name           string
		configExists   bool
		expectedStatus int
	}{
		{
			name:           "config exists",
			configExists:   true,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "no config exists",
			configExists:   false,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newTestHandler(t, dbClient)

			if tt.configExists {
				if err := handler.DB.SetDoc(ctx, "draft", "config", models.DraftConfig{
					Status:               "active",
					PickOrder:            []string{"user1", "user2"},
					PickTimeLimitSeconds: 90,
				}); err != nil {
					t.Fatalf("seed draft config: %v", err)
				}
			}

			req := httptest.NewRequest("POST", "/api/admin/draft/reset", nil)
			w := httptest.NewRecorder()

			handler.ResetDraft(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("ResetDraft() status = %v, want %v, body=%s", w.Code, tt.expectedStatus, w.Body.String())
			}
		})
	}
}

func TestDraftConfigMethods(t *testing.T) {
	tests := []struct {
		name             string
		pickOrder        []string
		currentPickIndex int
		expectedUID      string
	}{
		{
			name:             "first pick in round 1",
			pickOrder:        []string{"user1", "user2", "user3"},
			currentPickIndex: 0,
			expectedUID:      "user1",
		},
		{
			name:             "second pick in round 1",
			pickOrder:        []string{"user1", "user2", "user3"},
			currentPickIndex: 1,
			expectedUID:      "user2",
		},
		{
			name:             "first pick in round 2 (reverse order)",
			pickOrder:        []string{"user1", "user2", "user3"},
			currentPickIndex: 3,
			expectedUID:      "user3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &models.DraftConfig{
				PickOrder:        tt.pickOrder,
				CurrentPickIndex: tt.currentPickIndex,
			}

			uid := cfg.CurrentPickerUID()

			if uid != tt.expectedUID {
				t.Errorf("CurrentPickerUID() = %v, want %v", uid, tt.expectedUID)
			}
		})
	}
}

func TestComputeRound(t *testing.T) {
	tests := []struct {
		name             string
		pickOrder        []string
		currentPickIndex int
		expectedRound    int
	}{
		{
			name:             "first pick",
			pickOrder:        []string{"user1", "user2", "user3"},
			currentPickIndex: 0,
			expectedRound:    1,
		},
		{
			name:             "third pick",
			pickOrder:        []string{"user1", "user2", "user3"},
			currentPickIndex: 2,
			expectedRound:    1,
		},
		{
			name:             "fourth pick",
			pickOrder:        []string{"user1", "user2", "user3"},
			currentPickIndex: 3,
			expectedRound:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &models.DraftConfig{
				PickOrder:        tt.pickOrder,
				CurrentPickIndex: tt.currentPickIndex,
			}

			round := cfg.ComputeRound()

			if round != tt.expectedRound {
				t.Errorf("ComputeRound() = %v, want %v", round, tt.expectedRound)
			}
		})
	}
}

func TestCalculatePoints(t *testing.T) {
	tests := []struct {
		name     string
		stats    models.PlayerMatchStats
		position string
		expected int
	}{
		{
			name: "goal and assist",
			stats: models.PlayerMatchStats{
				Goals:   1,
				Assists: 1,
			},
			position: "FWD",
			expected: 8, // 5 + 3 = 8
		},
		{
			name: "goal and clean sheet (DEF)",
			stats: models.PlayerMatchStats{
				Goals:      0,
				Assists:    0,
				CleanSheet: true,
				TeamWin:    true,
			},
			position: "DEF",
			expected: 6, // 4 (clean sheet) + 2 (team win) = 6
		},
		{
			name: "yellow card",
			stats: models.PlayerMatchStats{
				YellowCards: 1,
			},
			position: "FWD",
			expected: -1, // -1 = 1 yellow card
		},
		{
			name: "red card",
			stats: models.PlayerMatchStats{
				RedCards: 1,
			},
			position: "FWD",
			expected: -3, // -3 = 1 red card
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			points := models.CalculatePoints(tt.stats, tt.position)

			if points != tt.expected {
				t.Errorf("CalculatePoints() = %v, want %v", points, tt.expected)
			}
		})
	}
}
