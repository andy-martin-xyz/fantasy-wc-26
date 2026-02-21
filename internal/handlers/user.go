package handlers

import (
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/andrewmartin/fantasy-league/internal/middleware"
	"github.com/andrewmartin/fantasy-league/internal/models"
)

// RegisterUser handles POST /api/user/register.
// Idempotent: creates the user doc on first call, skips creation if it exists.
// Pulls display name, email, photo from the verified Firebase token claims.
type registerRequest struct {
	TeamName string `json:"teamName"`
}

func (h *Handler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	uid := middleware.UIDFromContext(r.Context())

	var req registerRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.TeamName = strings.TrimSpace(req.TeamName)
	if req.TeamName == "" {
		writeError(w, http.StatusBadRequest, "teamName is required")
		return
	}

	// Check if user already exists (idempotent).
	var existing models.User
	found, err := h.DB.GetDoc(r.Context(), "users", uid, &existing)
	if err != nil {
		h.Log.Error("register: get user", "uid", uid, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if found {
		writeJSON(w, http.StatusOK, existing)
		return
	}

	user := models.User{
		UID:         uid,
		DisplayName: middleware.DisplayNameFromContext(r.Context()),
		Email:       middleware.EmailFromContext(r.Context()),
		PhotoURL:    middleware.PhotoURLFromContext(r.Context()),
		TeamName:    req.TeamName,
		CreatedAt:   time.Now().UTC(),
		IsAdmin:     false,
	}

	if err := h.DB.SetDoc(r.Context(), "users", uid, user); err != nil {
		h.Log.Error("register: set user doc", "uid", uid, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Also create an empty roster doc.
	roster := models.Roster{
		UserID:      uid,
		TeamName:    req.TeamName,
		Players:     []models.RosterPlayer{},
		TotalPoints: 0,
	}
	if err := h.DB.SetDoc(r.Context(), "rosters", uid, roster); err != nil {
		h.Log.Warn("register: set roster doc", "uid", uid, "err", err)
		// Non-fatal; roster can be created later.
	}

	h.Log.Info("user registered", "uid", uid, "teamName", req.TeamName)
	writeJSON(w, http.StatusCreated, user)
}

// UpdateTeamName handles PUT /api/user/team-name.
type updateTeamNameRequest struct {
	TeamName string `json:"teamName"`
}

func (h *Handler) UpdateTeamName(w http.ResponseWriter, r *http.Request) {
	uid := middleware.UIDFromContext(r.Context())

	var req updateTeamNameRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.TeamName = strings.TrimSpace(req.TeamName)
	if req.TeamName == "" {
		writeError(w, http.StatusBadRequest, "teamName cannot be empty")
		return
	}

	// Check uniqueness: scan all users for the same team name.
	iter := h.DB.FS.Collection("users").Where("teamName", "==", req.TeamName).Documents(r.Context())
	docs, err := iter.GetAll()
	if err != nil {
		h.Log.Error("update team name: query", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	for _, d := range docs {
		if d.Ref.ID != uid {
			writeError(w, http.StatusConflict, "team name already taken")
			return
		}
	}

	updates := []firestore.Update{
		{Path: "teamName", Value: req.TeamName},
	}
	if err := h.DB.UpdateDoc(r.Context(), "users", uid, updates); err != nil {
		h.Log.Error("update team name: update user", "uid", uid, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Keep roster in sync.
	_ = h.DB.UpdateDoc(r.Context(), "rosters", uid, []firestore.Update{
		{Path: "teamName", Value: req.TeamName},
	})

	writeJSON(w, http.StatusOK, map[string]string{"teamName": req.TeamName})
}

// GetUsers handles GET /api/users — public.
// Returns display name and team name for all registered users.
func (h *Handler) GetUsers(w http.ResponseWriter, r *http.Request) {
	docs, err := h.DB.FS.Collection("users").Documents(r.Context()).GetAll()
	if err != nil {
		h.Log.Error("get users", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	type publicUser struct {
		UID         string `json:"uid"`
		DisplayName string `json:"displayName"`
		TeamName    string `json:"teamName"`
		PhotoURL    string `json:"photoURL"`
	}

	users := make([]publicUser, 0, len(docs))
	for _, d := range docs {
		var u models.User
		if err := d.DataTo(&u); err != nil {
			continue
		}
		users = append(users, publicUser{
			UID:         u.UID,
			DisplayName: u.DisplayName,
			TeamName:    u.TeamName,
			PhotoURL:    u.PhotoURL,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"users": users})
}
