// Package handlers implements all HTTP route handlers.
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"firebase.google.com/go/v4/auth"
	"github.com/andrewmartin/fantasy-league/internal/db"
)

// Handler holds shared dependencies for all route handlers.
type Handler struct {
	DB          *db.Client
	Auth        *auth.Client
	Log         *slog.Logger
	adminEmails map[string]bool
}

// New creates a Handler with the given dependencies.
// Admin emails are read from the ADMIN_EMAILS environment variable (comma-separated).
func New(dbClient *db.Client, authClient *auth.Client, log *slog.Logger) *Handler {
	adminEmails := map[string]bool{}
	for _, e := range strings.Split(os.Getenv("ADMIN_EMAILS"), ",") {
		if e := strings.TrimSpace(strings.ToLower(e)); e != "" {
			adminEmails[e] = true
		}
	}
	return &Handler{DB: dbClient, Auth: authClient, Log: log, adminEmails: adminEmails}
}

// isAdminEmail reports whether the given email is in the admin list.
func (h *Handler) isAdminEmail(email string) bool {
	return h.adminEmails[strings.ToLower(email)]
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
