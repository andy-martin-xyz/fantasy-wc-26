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

// shuffleStrings returns a shuffled copy of s.
func shuffleStrings(s []string) []string {
	out := make([]string, len(s))
	copy(out, s)
	r := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec
	r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}
