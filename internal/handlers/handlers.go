// Package handlers implements all HTTP route handlers.
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"firebase.google.com/go/v4/auth"
	"github.com/andrewmartin/fantasy-league/internal/db"
)

// Handler holds shared dependencies for all route handlers.
type Handler struct {
	DB   *db.Client
	Auth *auth.Client
	Log  *slog.Logger
}

// New creates a Handler with the given dependencies.
func New(dbClient *db.Client, authClient *auth.Client, log *slog.Logger) *Handler {
	return &Handler{DB: dbClient, Auth: authClient, Log: log}
}

// --- Response helpers -------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// headers already sent; nothing more we can do
		return
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}
