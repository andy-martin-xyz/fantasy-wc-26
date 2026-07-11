// cmd/fixtures loads World Cup match docs into Firestore from ESPN's schedule,
// so they exist (with espnEventId) ready for scoring. Mirrors the
// POST /api/admin/fixtures/import handler, but runnable with a service account.
//
// Knockout fixtures with placeholder teams ("Group A 2nd Place") are skipped
// until ESPN fills in the real teams; just re-run as the tournament progresses.
//
// Usage:
//
//	SEED_PRODUCTION=true FIREBASE_PROJECT_ID=andy-personal-1bb38 \
//	  GOOGLE_APPLICATION_CREDENTIALS=/path/sa.json go run ./cmd/fixtures
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"cloud.google.com/go/firestore"
	"github.com/andrewmartin/fantasy-league/internal/espn"
	"github.com/andrewmartin/fantasy-league/internal/models"
)

func main() {
	dates := flag.String("dates", "20260611-20260719", "ESPN dates value (single day or YYYYMMDD-YYYYMMDD range)")
	flag.Parse()

	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" && os.Getenv("SEED_PRODUCTION") != "true" {
		log.Fatal("set FIRESTORE_EMULATOR_HOST for the emulator, or SEED_PRODUCTION=true for prod")
	}
	projectID := os.Getenv("FIREBASE_PROJECT_ID")
	if projectID == "" {
		projectID = "andy-personal-1bb38"
	}

	ctx := context.Background()
	fixtures, err := espn.FetchFixtures(ctx, *dates)
	if err != nil {
		log.Fatalf("fetch fixtures: %v", err)
	}
	fmt.Printf("ESPN returned %d fixtures for %s\n", len(fixtures), *dates)

	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("firestore: %v", err)
	}
	defer client.Close()

	imported, skipped := 0, 0
	for _, f := range fixtures {
		home := models.NormalizeTeamName(f.HomeTeam)
		away := models.NormalizeTeamName(f.AwayTeam)
		if home == "" || away == "" {
			skipped++
			continue
		}
		match := models.Match{
			ID:          f.EventID,
			HomeTeam:    home,
			AwayTeam:    away,
			Date:        f.Date,
			Status:      statusFromState(f.State),
			ESPNEventID: f.EventID,
		}
		// Preserve scoring state on re-run.
		var existing models.Match
		if snap, err := client.Collection("matches").Doc(f.EventID).Get(ctx); err == nil {
			if snap.DataTo(&existing) == nil && existing.ScoringProcessed {
				match.ScoringProcessed = true
				match.Status = "complete"
				match.HomeScore = existing.HomeScore
				match.AwayScore = existing.AwayScore
			}
		}
		if _, err := client.Collection("matches").Doc(f.EventID).Set(ctx, match); err != nil {
			fmt.Printf("  ✗ %s v %s: %v\n", home, away, err)
			continue
		}
		fmt.Printf("  ✓ %s  %-22s v %-22s\n", f.Date.Format("Jan 02 15:04"), home, away)
		imported++
	}
	fmt.Printf("\nLoaded %d matches, skipped %d placeholder/knockout fixtures.\n", imported, skipped)
}

func statusFromState(state string) string {
	switch state {
	case "in":
		return "live"
	case "post":
		return "complete"
	default:
		return "upcoming"
	}
}
