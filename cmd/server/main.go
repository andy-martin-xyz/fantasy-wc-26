package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	firebase "firebase.google.com/go/v4"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"google.golang.org/api/option"

	"github.com/andrewmartin/fantasy-league/internal/db"
	"github.com/andrewmartin/fantasy-league/internal/handlers"
	"github.com/andrewmartin/fantasy-league/internal/middleware"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx := context.Background()

	// --- Firebase / Firestore init -----------------------------------
	dbClient, err := db.New(ctx)
	if err != nil {
		log.Error("connect to Firestore", "err", err)
		os.Exit(1)
	}
	defer dbClient.Close()

	// Firebase app for Auth client.
	projectID := os.Getenv("FIREBASE_PROJECT_ID")
	firebaseConf := &firebase.Config{ProjectID: projectID}
	var firebaseApp *firebase.App
	if credFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); credFile != "" {
		firebaseApp, err = firebase.NewApp(ctx, firebaseConf, option.WithCredentialsFile(credFile))
	} else {
		firebaseApp, err = firebase.NewApp(ctx, firebaseConf)
	}
	if err != nil {
		log.Error("init firebase app", "err", err)
		os.Exit(1)
	}

	authClient, err := firebaseApp.Auth(ctx)
	if err != nil {
		log.Error("init firebase auth", "err", err)
		os.Exit(1)
	}

	// --- Handler + router --------------------------------------------
	h := handlers.New(dbClient, authClient, log)

	r := chi.NewRouter()
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Recoverer)
	r.Use(corsMiddleware(os.Getenv("ALLOWED_ORIGIN"), log))
	r.Use(chimiddleware.Timeout(30 * time.Second))

	// Public routes
	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
	r.Get("/api/leaderboard", h.GetLeaderboard)
	r.Get("/api/teams", h.GetTeams)
	r.Get("/api/team/{uid}", h.GetTeam)
	r.Get("/api/draft/status", h.GetDraftStatus)
	r.Get("/api/players", h.GetPlayers)
	r.Get("/api/users", h.GetUsers)

	// Authenticated routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.Authenticate(authClient))
		r.Post("/api/user/register", h.RegisterUser)
		r.Put("/api/user/team-name", h.UpdateTeamName)
		r.Post("/api/draft/pick", h.SubmitPick)
		r.Post("/api/draft/suggest", h.SuggestPick)
	})

	// Admin routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.Authenticate(authClient))
		r.Use(middleware.RequireAdmin(dbClient))
		r.Post("/api/admin/draft/set-order", h.SetDraftOrder)
		r.Post("/api/admin/draft/start", h.StartDraft)
		r.Post("/api/admin/draft/pause", h.PauseDraft)
		r.Post("/api/admin/draft/resume", h.ResumeDraft)
		r.Post("/api/admin/draft/undo-pick", h.UndoLastPick)
		r.Post("/api/admin/draft/reset", h.ResetDraft)
		r.Post("/api/admin/players/import", h.ImportPlayers)
		r.Put("/api/admin/match/{matchId}", h.UpsertMatch)
		r.Get("/api/admin/matches", h.GetMatches)
		r.Post("/api/admin/fixtures/import", h.ImportFixtures)
		r.Post("/api/admin/scores/process", h.ProcessScores)
		r.Post("/api/admin/scores/fetch/{matchId}", h.FetchScores)
	})

	// --- Server ------------------------------------------------------
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Info("server starting", "addr", addr)

	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("server error", "err", err)
		os.Exit(1)
	}
}

// corsMiddleware allows requests from the configured origin (and always from localhost).
func corsMiddleware(allowedOrigin string, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			log.Debug("cors", "origin", origin, "method", r.Method, "path", r.URL.Path)
			allowed := origin != "" // allow any origin that sends one (dev-friendly)
			if !allowed && allowedOrigin != "" {
				allowed = origin == allowedOrigin
			}
			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
