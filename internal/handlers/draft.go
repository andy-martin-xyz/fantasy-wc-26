package handlers

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/andrewmartin/fantasy-league/internal/middleware"
	"github.com/andrewmartin/fantasy-league/internal/models"
)

// GetDraftStatus handles GET /api/draft/status — public.
func (h *Handler) GetDraftStatus(w http.ResponseWriter, r *http.Request) {
	var cfg models.DraftConfig
	found, err := h.DB.GetDoc(r.Context(), "draft", "config", &cfg)
	if err != nil {
		h.Log.Error("get draft status", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, map[string]string{"status": "pending"})
		return
	}

	resp := map[string]any{
		"status":           cfg.Status,
		"round":            cfg.ComputeRound(),
		"currentPickIndex": cfg.CurrentPickIndex,
		"currentPickerUID": cfg.CurrentPickerUID(),
		"pickOrder":        cfg.PickOrder,
		"pickDeadline":     cfg.CurrentPickDeadline,
	}
	writeJSON(w, http.StatusOK, resp)
}

// SetDraftOrder handles POST /api/admin/draft/set-order.
type setOrderRequest struct {
	Order     []string `json:"order"`
	Randomize bool     `json:"randomize"`
}

func (h *Handler) SetDraftOrder(w http.ResponseWriter, r *http.Request) {
	var req setOrderRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var order []string

	if req.Randomize {
		docs, err := h.DB.FS.Collection("users").Documents(r.Context()).GetAll()
		if err != nil {
			h.Log.Error("set order: list users", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for _, d := range docs {
			order = append(order, d.Ref.ID)
		}
		order = shuffleStrings(order)
	} else {
		if len(req.Order) == 0 {
			writeError(w, http.StatusBadRequest, "order must be non-empty or set randomize:true")
			return
		}
		order = req.Order
	}

	var cfg models.DraftConfig
	found, _ := h.DB.GetDoc(r.Context(), "draft", "config", &cfg)
	if !found {
		cfg = models.DraftConfig{
			Status:               "pending",
			PickTimeLimitSeconds: 90,
		}
	}
	cfg.PickOrder = order

	if err := h.DB.SetDoc(r.Context(), "draft", "config", cfg); err != nil {
		h.Log.Error("set order: save config", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"pickOrder": order})
}

// StartDraft handles POST /api/admin/draft/start.
func (h *Handler) StartDraft(w http.ResponseWriter, r *http.Request) {
	var cfg models.DraftConfig
	found, err := h.DB.GetDoc(r.Context(), "draft", "config", &cfg)
	if err != nil || !found {
		writeError(w, http.StatusBadRequest, "draft not configured — set pick order first")
		return
	}
	if len(cfg.PickOrder) == 0 {
		writeError(w, http.StatusBadRequest, "pick order is empty")
		return
	}
	if cfg.Status == "complete" {
		writeError(w, http.StatusBadRequest, "draft is already complete")
		return
	}

	cfg.Status = "active"
	cfg.CurrentPickIndex = 0
	cfg.Round = 1
	cfg.CurrentPickDeadline = time.Now().UTC().Add(time.Duration(cfg.PickTimeLimitSeconds) * time.Second)

	if err := h.DB.SetDoc(r.Context(), "draft", "config", cfg); err != nil {
		h.Log.Error("start draft: save config", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.Log.Info("draft started")
	writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
}

// PauseDraft handles POST /api/admin/draft/pause.
func (h *Handler) PauseDraft(w http.ResponseWriter, r *http.Request) {
	h.setDraftStatus(w, r, "active", "paused")
}

// ResumeDraft handles POST /api/admin/draft/resume.
func (h *Handler) ResumeDraft(w http.ResponseWriter, r *http.Request) {
	h.setDraftStatus(w, r, "paused", "active")
}

func (h *Handler) setDraftStatus(w http.ResponseWriter, r *http.Request, requiredCurrent, newStatus string) {
	var cfg models.DraftConfig
	found, err := h.DB.GetDoc(r.Context(), "draft", "config", &cfg)
	if err != nil || !found {
		writeError(w, http.StatusBadRequest, "draft config not found")
		return
	}
	if cfg.Status != requiredCurrent {
		writeError(w, http.StatusBadRequest, "draft is not in "+requiredCurrent+" state")
		return
	}
	cfg.Status = newStatus
	if newStatus == "active" {
		cfg.CurrentPickDeadline = time.Now().UTC().Add(time.Duration(cfg.PickTimeLimitSeconds) * time.Second)
	}
	if err := h.DB.SetDoc(r.Context(), "draft", "config", cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": newStatus})
}

// SubmitPick handles POST /api/draft/pick — authenticated, must be caller's turn.
type pickRequest struct {
	PlayerID string `json:"playerId"`
}

func (h *Handler) SubmitPick(w http.ResponseWriter, r *http.Request) {
	uid := middleware.UIDFromContext(r.Context())

	var req pickRequest
	if err := decode(r, &req); err != nil || req.PlayerID == "" {
		writeError(w, http.StatusBadRequest, "playerId is required")
		return
	}

	// Firestore transaction ensures atomicity — no double picks, no out-of-turn picks.
	txErr := h.DB.FS.RunTransaction(r.Context(), func(ctx context.Context, tx *firestore.Transaction) error {
		cfgRef := h.DB.FS.Collection("draft").Doc("config")
		cfgSnap, err := tx.Get(cfgRef)
		if err != nil {
			return fmt.Errorf("draft config not found")
		}
		var cfg models.DraftConfig
		if err := cfgSnap.DataTo(&cfg); err != nil {
			return fmt.Errorf("decode draft config")
		}
		if cfg.Status != "active" {
			return fmt.Errorf("draft is not active")
		}
		if cfg.CurrentPickerUID() != uid {
			return fmt.Errorf("not your turn")
		}

		playerRef := h.DB.FS.Collection("players").Doc(req.PlayerID)
		playerSnap, err := tx.Get(playerRef)
		if err != nil {
			return fmt.Errorf("player not found")
		}
		var player models.Player
		if err := playerSnap.DataTo(&player); err != nil {
			return fmt.Errorf("decode player")
		}
		if player.Drafted {
			return fmt.Errorf("player already drafted")
		}

		rosterRef := h.DB.FS.Collection("rosters").Doc(uid)
		rosterSnap, err := tx.Get(rosterRef)
		var roster models.Roster
		if err == nil {
			_ = rosterSnap.DataTo(&roster)
		}

		posCount := 0
		for _, rp := range roster.Players {
			if rp.Position == player.Position {
				posCount++
			}
		}
		limit := models.PositionLimits[player.Position]
		if posCount >= limit {
			return fmt.Errorf("position limit reached: max %d %s", limit, player.Position)
		}

		pickNumber := cfg.CurrentPickIndex + 1
		round := cfg.ComputeRound()

		// Write pick sub-collection: draft/picks/{pickNumber}
		pickRef := h.DB.FS.Collection("draft").Doc("picks").Collection("items").Doc(fmt.Sprintf("%d", pickNumber))
		pick := models.DraftPick{
			UserID:     uid,
			PlayerID:   req.PlayerID,
			PlayerName: player.Name,
			Round:      round,
			PickNumber: pickNumber,
			Timestamp:  time.Now().UTC(),
		}
		if err := tx.Set(pickRef, pick); err != nil {
			return err
		}

		if err := tx.Update(playerRef, []firestore.Update{
			{Path: "drafted", Value: true},
			{Path: "draftedBy", Value: uid},
		}); err != nil {
			return err
		}

		updatedPlayers := append(roster.Players, models.RosterPlayer{
			PlayerID: req.PlayerID,
			Name:     player.Name,
			Country:  player.Country,
			Position: player.Position,
		})
		if err := tx.Update(rosterRef, []firestore.Update{
			{Path: "players", Value: updatedPlayers},
		}); err != nil {
			return err
		}

		nextIndex := cfg.CurrentPickIndex + 1
		totalPicks := len(cfg.PickOrder) * 11
		newStatus := cfg.Status
		if nextIndex >= totalPicks {
			newStatus = "complete"
		}
		if err := tx.Update(cfgRef, []firestore.Update{
			{Path: "currentPickIndex", Value: nextIndex},
			{Path: "status", Value: newStatus},
			{Path: "round", Value: nextIndex/len(cfg.PickOrder) + 1},
			{Path: "currentPickDeadline", Value: time.Now().UTC().Add(time.Duration(cfg.PickTimeLimitSeconds) * time.Second)},
		}); err != nil {
			return err
		}

		return nil
	})

	if txErr != nil {
		h.Log.Warn("submit pick failed", "uid", uid, "player", req.PlayerID, "err", txErr)
		writeError(w, http.StatusBadRequest, txErr.Error())
		return
	}

	h.Log.Info("pick submitted", "uid", uid, "player", req.PlayerID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// UndoLastPick handles POST /api/admin/draft/undo-pick.
// Removes the most recent pick, resets that player to undrafted, removes them
// from the drafter's roster, and decrements the pick index.
func (h *Handler) UndoLastPick(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Find the last pick by descending pickNumber.
	pickDocs, err := h.DB.FS.Collection("draft").Doc("picks").Collection("items").
		OrderBy("pickNumber", firestore.Desc).Limit(1).Documents(ctx).GetAll()
	if err != nil {
		h.Log.Error("undo pick: query picks", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(pickDocs) == 0 {
		writeError(w, http.StatusBadRequest, "no picks to undo")
		return
	}

	var lastPick models.DraftPick
	if err := pickDocs[0].DataTo(&lastPick); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode last pick")
		return
	}

	txErr := h.DB.FS.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		cfgRef := h.DB.FS.Collection("draft").Doc("config")
		cfgSnap, err := tx.Get(cfgRef)
		if err != nil {
			return fmt.Errorf("draft config not found")
		}
		var cfg models.DraftConfig
		if err := cfgSnap.DataTo(&cfg); err != nil {
			return err
		}

		rosterRef := h.DB.FS.Collection("rosters").Doc(lastPick.UserID)
		rosterSnap, err := tx.Get(rosterRef)
		if err != nil {
			return fmt.Errorf("roster not found for user %s", lastPick.UserID)
		}
		var roster models.Roster
		if err := rosterSnap.DataTo(&roster); err != nil {
			return err
		}

		// Delete the pick document.
		pickRef := h.DB.FS.Collection("draft").Doc("picks").Collection("items").Doc(fmt.Sprintf("%d", lastPick.PickNumber))
		if err := tx.Delete(pickRef); err != nil {
			return err
		}

		// Reset the player to undrafted.
		playerRef := h.DB.FS.Collection("players").Doc(lastPick.PlayerID)
		if err := tx.Update(playerRef, []firestore.Update{
			{Path: "drafted", Value: false},
			{Path: "draftedBy", Value: ""},
		}); err != nil {
			return err
		}

		// Remove the player from the drafter's roster.
		updatedPlayers := make([]models.RosterPlayer, 0, len(roster.Players))
		for _, rp := range roster.Players {
			if rp.PlayerID != lastPick.PlayerID {
				updatedPlayers = append(updatedPlayers, rp)
			}
		}
		if err := tx.Update(rosterRef, []firestore.Update{
			{Path: "players", Value: updatedPlayers},
		}); err != nil {
			return err
		}

		// Decrement pick index; reactivate draft if it was complete.
		prevIndex := cfg.CurrentPickIndex - 1
		if prevIndex < 0 {
			prevIndex = 0
		}
		newStatus := cfg.Status
		if newStatus == "complete" {
			newStatus = "active"
		}
		round := 1
		if len(cfg.PickOrder) > 0 {
			round = prevIndex/len(cfg.PickOrder) + 1
		}
		return tx.Update(cfgRef, []firestore.Update{
			{Path: "currentPickIndex", Value: prevIndex},
			{Path: "status", Value: newStatus},
			{Path: "round", Value: round},
			{Path: "currentPickDeadline", Value: time.Now().UTC().Add(time.Duration(cfg.PickTimeLimitSeconds) * time.Second)},
		})
	})

	if txErr != nil {
		h.Log.Error("undo pick failed", "err", txErr)
		writeError(w, http.StatusInternalServerError, txErr.Error())
		return
	}

	h.Log.Info("pick undone", "player", lastPick.PlayerID, "user", lastPick.UserID, "pick#", lastPick.PickNumber)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"undone":     lastPick.PlayerName,
		"pickNumber": lastPick.PickNumber,
	})
}

// ResetDraft handles POST /api/admin/draft/reset.
// Wipes all picks, resets all players to undrafted, clears all rosters,
// clears the leaderboard, and sets draft status back to pending.
// The pick order and time limit are preserved so the draft can be restarted.
func (h *Handler) ResetDraft(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var cfg models.DraftConfig
	found, err := h.DB.GetDoc(ctx, "draft", "config", &cfg)
	if err != nil || !found {
		writeError(w, http.StatusBadRequest, "draft config not found — nothing to reset")
		return
	}

	batch := h.DB.FS.Batch()

	// Delete all pick documents.
	pickDocs, err := h.DB.FS.Collection("draft").Doc("picks").Collection("items").Documents(ctx).GetAll()
	if err != nil {
		h.Log.Error("reset draft: list picks", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	for _, pickDoc := range pickDocs {
		batch.Delete(pickDoc.Ref)
	}

	// Reset all drafted players to undrafted.
	playerDocs, err := h.DB.FS.Collection("players").Where("drafted", "==", true).Documents(ctx).GetAll()
	if err != nil {
		h.Log.Error("reset draft: list players", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	for _, playerDoc := range playerDocs {
		batch.Update(playerDoc.Ref, []firestore.Update{
			{Path: "drafted", Value: false},
			{Path: "draftedBy", Value: ""},
		})
	}

	// Clear all rosters.
	rosterDocs, err := h.DB.FS.Collection("rosters").Documents(ctx).GetAll()
	if err != nil {
		h.Log.Error("reset draft: list rosters", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	for _, rosterDoc := range rosterDocs {
		batch.Update(rosterDoc.Ref, []firestore.Update{
			{Path: "players", Value: []models.RosterPlayer{}},
			{Path: "totalPoints", Value: 0},
		})
	}

	// Clear the leaderboard standings.
	leaderboardRef := h.DB.FS.Collection("leaderboard").Doc("current")
	batch.Set(leaderboardRef, models.Leaderboard{Standings: []models.LeaderboardEntry{}})

	// Reset draft config to pending, preserving pick order and time limit.
	cfgRef := h.DB.FS.Collection("draft").Doc("config")
	batch.Update(cfgRef, []firestore.Update{
		{Path: "status", Value: "pending"},
		{Path: "currentPickIndex", Value: 0},
		{Path: "round", Value: 1},
		{Path: "currentPickDeadline", Value: time.Time{}},
	})

	if _, err := batch.Commit(ctx); err != nil {
		h.Log.Error("reset draft: commit batch", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error committing reset")
		return
	}

	h.Log.Info("draft reset",
		"picks_deleted", len(pickDocs),
		"players_reset", len(playerDocs),
		"rosters_cleared", len(rosterDocs),
	)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "reset",
		"picksDeleted":   len(pickDocs),
		"playersReset":   len(playerDocs),
		"rostersCleared": len(rosterDocs),
	})
}

// shuffleStrings returns a shuffled copy of s.
func shuffleStrings(s []string) []string {
	out := make([]string, len(s))
	copy(out, s)
	r := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec
	r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}
