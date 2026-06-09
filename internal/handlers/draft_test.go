package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/andrewmartin/fantasy-league/internal/db"
	"github.com/andrewmartin/fantasy-league/internal/middleware"
	"github.com/andrewmartin/fantasy-league/internal/models"
	"google.golang.org/api/option"
)

// MockFirestoreClient implements the db.Client interface for testing
type MockFirestoreClient struct {
	db.Client
	docs map[string]map[string]interface{}
}

func (m *MockFirestoreClient) GetDoc(ctx context.Context, collection, docID string, dest interface{}) (bool, error) {
	key := fmt.Sprintf("%s/%s", collection, docID)
	doc, exists := m.docs[key]
	if !exists {
		return false, nil
	}

	// Convert map to JSON and back to populate dest
	jsonBytes, err := json.Marshal(doc)
	if err != nil {
		return true, err
	}

	err = json.Unmarshal(jsonBytes, dest)
	return true, err
}

func (m *MockFirestoreClient) SetDoc(ctx context.Context, collection, docID string, data interface{}) error {
	key := fmt.Sprintf("%s/%s", collection, docID)

	// Convert data to map for storage
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	var doc map[string]interface{}
	err = json.Unmarshal(jsonBytes, &doc)
	if err != nil {
		return err
	}

	if m.docs == nil {
		m.docs = make(map[string]map[string]interface{})
	}
	m.docs[key] = doc
	return nil
}

func (m *MockFirestoreClient) FS() *firestore.Client {
	// Return a mock Firestore client for testing
	return nil
}

func (m *MockFirestoreClient) RunTransaction(ctx context.Context, f func(context.Context, *firestore.Transaction) error) error {
	// For testing, we'll just run the function directly
	return f(ctx, nil)
}

func TestGetDraftStatus(t *testing.T) {
	tests := []struct {
		name           string
		configExists   bool
		configData     models.DraftConfig
		expectedStatus int
	}{
		{
			name:         "no config exists",
			configExists: false,
			expectedStatus: http.StatusOK,
		},
		{
			name: "config exists with pending status",
			configExists: true,
			configData: models.DraftConfig{
				Status: "pending",
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "config exists with active status",
			configExists: true,
			configData: models.DraftConfig{
				Status: "active",
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB := &MockFirestoreClient{}

			if tt.configExists {
				key := "draft/config"
				mockDB.docs = make(map[string]map[string]interface{})
				mockDB.docs[key] = map[string]interface{}{
					"status": tt.configData.Status,
				}
			}

			handler := &Handler{
				DB: mockDB,
			}

			req := httptest.NewRequest("GET", "/api/draft/status", nil)
			w := httptest.NewRecorder()

			handler.GetDraftStatus(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("GetDraftStatus() status = %v, want %v", w.Code, tt.expectedStatus)
			}
		})
	}
}

func TestSetDraftOrder(t *testing.T) {
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
			name: "randomize true with users",
			requestBody: setOrderRequest{
				Randomize: true,
			},
			usersExist:     true,
			expectedStatus: http.StatusOK,
		},
		{
			name: "randomize true without users",
			requestBody: setOrderRequest{
				Randomize: true,
			},
			expectedStatus: http.StatusOK, // Should still succeed even with no users
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
			mockDB := &MockFirestoreClient{}

			if tt.usersExist {
				mockDB.docs = make(map[string]map[string]interface{})
				mockDB.docs["users/user1"] = map[string]interface{}{
					"uid": "user1",
				}
				mockDB.docs["users/user2"] = map[string]interface{}{
					"uid": "user2",
				}
			}

			handler := &Handler{
				DB: mockDB,
			}

			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest("POST", "/api/admin/draft/set-order", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.SetDraftOrder(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("SetDraftOrder() status = %v, want %v", w.Code, tt.expectedStatus)
			}
		})
	}
}

func TestStartDraft(t *testing.T) {
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
			name: "config exists but empty pick order",
			configExists: true,
			configData: models.DraftConfig{
				PickOrder: []string{},
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "config exists with valid pick order",
			configExists: true,
			configData: models.DraftConfig{
				PickOrder: []string{"user1", "user2"},
			},
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
			mockDB := &MockFirestoreClient{}

			if tt.configExists {
				key := "draft/config"
				mockDB.docs = make(map[string]map[string]interface{})
				mockDB.docs[key] = map[string]interface{}{
					"pickOrder": tt.configData.PickOrder,
					"status":    tt.configData.Status,
				}
			}

			handler := &Handler{
				DB: mockDB,
			}

			req := httptest.NewRequest("POST", "/api/admin/draft/start", nil)
			w := httptest.NewRecorder()

			handler.StartDraft(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("StartDraft() status = %v, want %v", w.Code, tt.expectedStatus)
			}
		})
	}
}

func TestSetDraftStatus(t *testing.T) {
	tests := []struct {
		name           string
		currentStatus  string
		requiredStatus string
		newStatus      string
		expectedStatus int
	}{
		{
			name:           "valid status change",
			currentStatus:  "active",
			requiredStatus: "active",
			newStatus:      "paused",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid status change",
			currentStatus:  "paused",
			requiredStatus: "active",
			newStatus:      "paused",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "valid resume",
			currentStatus:  "paused",
			requiredStatus: "paused",
			newStatus:      "active",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB := &MockFirestoreClient{}

			key := "draft/config"
			mockDB.docs = make(map[string]map[string]interface{})
			mockDB.docs[key] = map[string]interface{}{
				"status": tt.currentStatus,
			}

			handler := &Handler{
				DB: mockDB,
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
				t.Errorf("setDraftStatus() status = %v, want %v", w.Code, tt.expectedStatus)
			}
		})
	}
}

func TestSubmitPick(t *testing.T) {
	tests := []struct {
		name              string
		uid               string
		playerID          string
		playerExists      bool
		playerDrafted     bool
		playerPosition    string
		rosterPlayers     []models.RosterPlayer
		draftConfig       models.DraftConfig
		expectedStatus    int
	}{
		{
			name:           "valid pick",
			uid:            "user1",
			playerID:       "player1",
			playerExists:   true,
			playerDrafted:  false,
			playerPosition: "FWD",
			rosterPlayers:  []models.RosterPlayer{},
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
			playerExists:   true,
			playerDrafted:  true,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "not user's turn",
			uid:            "user2",
			playerID:       "player1",
			playerExists:   true,
			playerDrafted:  false,
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
			playerExists:   true,
			playerDrafted:  false,
			playerPosition: "FWD",
			rosterPlayers: []models.RosterPlayer{
				{PlayerID: "player2", Position: "FWD"},
				{PlayerID: "player3", Position: "FWD"},
			},
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
			mockDB := &MockFirestoreClient{}

			if tt.playerExists {
				key := "players/player1"
				mockDB.docs = make(map[string]map[string]interface{})
				mockDB.docs[key] = map[string]interface{}{
					"id":         "player1",
					"name":       "Player 1",
					"position":   tt.playerPosition,
					"drafted":   tt.playerDrafted,
					"draftedBy":  "",
				}
			}

			if tt.draftConfig.Status != "" {
				key := "draft/config"
				mockDB.docs[key] = map[string]interface{}{
					"status":               tt.draftConfig.Status,
					"currentPickIndex":     tt.draftConfig.CurrentPickIndex,
					"pickOrder":            tt.draftConfig.PickOrder,
					"pickTimeLimitSeconds": tt.draftConfig.PickTimeLimitSeconds,
				}
			}

			handler := &Handler{
				DB: mockDB,
			}

			body, _ := json.Marshal(pickRequest{PlayerID: tt.playerID})
			req := httptest.NewRequest("POST", "/api/draft/pick", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			// Add UID to context
			ctx := context.WithValue(req.Context(), middleware.CtxUID, tt.uid)
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()

			handler.SubmitPick(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("SubmitPick() status = %v, want %v", w.Code, tt.expectedStatus)
			}
		})
	}
}

func TestUndoLastPick(t *testing.T) {
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
			mockDB := &MockFirestoreClient{}

			if tt.pickExists {
				mockDB.docs = make(map[string]map[string]interface{})
				mockDB.docs["draft/picks/items/1"] = map[string]interface{}{
					"userId":     "user1",
					"playerId":   "player1",
					"playerName": "Player 1",
					"pickNumber": 1,
				}
			}

			handler := &Handler{
				DB: mockDB,
			}

			req := httptest.NewRequest("POST", "/api/admin/draft/undo-pick", nil)
			w := httptest.NewRecorder()

			handler.UndoLastPick(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("UndoLastPick() status = %v, want %v", w.Code, tt.expectedStatus)
			}
		})
	}
}

func TestResetDraft(t *testing.T) {
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
			mockDB := &MockFirestoreClient{}

			if tt.configExists {
				key := "draft/config"
				mockDB.docs = make(map[string]map[string]interface{})
				mockDB.docs[key] = map[string]interface{}{
					"status": "active",
				}
			}

			handler := &Handler{
				DB: mockDB,
			}

			req := httptest.NewRequest("POST", "/api/admin/draft/reset", nil)
			w := httptest.NewRecorder()

			handler.ResetDraft(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("ResetDraft() status = %v, want %v", w.Code, tt.expectedStatus)
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
		name       string
		stats      models.PlayerMatchStats
		position   string
		expected   int
	}{
		{
			name: "goal and assist",
			stats: models.PlayerMatchStats{
				Goals:   1,
				Assists: 1,
			},
			position:  "FWD",
			expected:  8, // 5 + 3 = 8
		},
		{
			name: "goal and clean sheet (DEF)",
			stats: models.PlayerMatchStats{
				Goals:       0,
				Assists:     0,
				CleanSheet:  true,
				TeamWin:     true,
			},
			position:  "DEF",
			expected:  6, // 4 (clean sheet) + 2 (team win) = 6
		},
		{
			name: "yellow card",
			stats: models.PlayerMatchStats{
				YellowCards: 1,
			},
			position:  "FWD",
			expected:  -1, // -1 = 1 yellow card
		},
		{
			name: "red card",
			stats: models.PlayerMatchStats{
				RedCards: 1,
			},
			position:  "FWD",
			expected:  -3, // -3 = 1 red card
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