// cmd/seed seeds the Firestore emulator with test players for local development.
// Run with: go run ./cmd/seed
// Requires FIRESTORE_EMULATOR_HOST to be set (refuses to run against production).
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"cloud.google.com/go/firestore"
	"github.com/andrewmartin/fantasy-league/internal/models"
)

// playerDocID matches the logic in internal/handlers/players.go.
func playerDocID(name, country string) string {
	slug := func(s string) string {
		s = strings.ToLower(s)
		var out []rune
		for _, r := range s {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				out = append(out, r)
			} else {
				out = append(out, '-')
			}
		}
		result := strings.Join(strings.Fields(string(out)), "-")
		return strings.Trim(result, "-")
	}
	return slug(name) + "_" + slug(country)
}

type seedPlayer struct {
	Name     string
	Country  string
	Position string
	ClubTeam string
	Rating   int
}

var testPlayers = []seedPlayer{
	// ---- Goalkeepers ----
	{"Alisson",             "Brazil",      "GK",  "Liverpool",     88},
	{"Marc-Andre ter Stegen","Germany",    "GK",  "Barcelona",     87},
	{"Thibaut Courtois",    "Belgium",     "GK",  "Real Madrid",   88},
	{"Jordan Pickford",     "England",     "GK",  "Everton",       82},
	{"Unai Simon",          "Spain",       "GK",  "Athletic Club", 81},

	// ---- Defenders ----
	{"Virgil van Dijk",     "Netherlands", "DEF", "Liverpool",     89},
	{"Achraf Hakimi",       "Morocco",     "DEF", "PSG",           86},
	{"Theo Hernandez",      "France",      "DEF", "AC Milan",      85},
	{"Ruben Dias",          "Portugal",    "DEF", "Man City",      87},
	{"Eder Militao",        "Brazil",      "DEF", "Real Madrid",   85},
	{"Joao Cancelo",        "Portugal",    "DEF", "Barcelona",     84},
	{"Antonio Rudiger",     "Germany",     "DEF", "Real Madrid",   84},
	{"Marquinhos",          "Brazil",      "DEF", "PSG",           85},
	{"Kyle Walker",         "England",     "DEF", "Man City",      83},
	{"Jules Kounde",        "France",      "DEF", "Barcelona",     84},
	{"Ronald Araujo",       "Uruguay",     "DEF", "Barcelona",     84},

	// ---- Midfielders ----
	{"Kevin De Bruyne",     "Belgium",     "MID", "Man City",      91},
	{"Luka Modric",         "Croatia",     "MID", "Real Madrid",   87},
	{"Pedri",               "Spain",       "MID", "Barcelona",     88},
	{"Jude Bellingham",     "England",     "MID", "Real Madrid",   89},
	{"Federico Valverde",   "Uruguay",     "MID", "Real Madrid",   86},
	{"Gavi",                "Spain",       "MID", "Barcelona",     87},
	{"Bruno Fernandes",     "Portugal",    "MID", "Man United",    85},
	{"Declan Rice",         "England",     "MID", "Arsenal",       84},
	{"Alexis Mac Allister", "Argentina",   "MID", "Liverpool",     83},
	{"Bernardo Silva",      "Portugal",    "MID", "Man City",      86},

	// ---- Forwards ----
	{"Kylian Mbappe",       "France",      "FWD", "Real Madrid",   92},
	{"Erling Haaland",      "Norway",      "FWD", "Man City",      93},
	{"Vinicius Jr",         "Brazil",      "FWD", "Real Madrid",   91},
	{"Lamine Yamal",        "Spain",       "FWD", "Barcelona",     88},
	{"Harry Kane",          "England",     "FWD", "Bayern Munich", 90},
	{"Mohamed Salah",       "Egypt",       "FWD", "Liverpool",     89},
	{"Antoine Griezmann",   "France",      "FWD", "Atletico Madrid",86},
	{"Son Heung-min",       "South Korea", "FWD", "Tottenham",     87},
	{"Marcus Rashford",     "England",     "FWD", "Man United",    84},
}

func main() {
	emulator := os.Getenv("FIRESTORE_EMULATOR_HOST") != ""
	production := os.Getenv("SEED_PRODUCTION") == "true"
	if !emulator && !production {
		log.Fatal("Set FIRESTORE_EMULATOR_HOST for the local emulator, or SEED_PRODUCTION=true to seed production Firestore")
	}
	if production && !emulator {
		fmt.Println("WARNING: seeding PRODUCTION Firestore")
	}

	projectID := os.Getenv("FIREBASE_PROJECT_ID")
	if projectID == "" {
		projectID = "andy-personal-1bb38"
	}

	ctx := context.Background()
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("create firestore client: %v", err)
	}
	defer client.Close()

	fmt.Printf("Seeding %d players into project %q...\n\n", len(testPlayers), projectID)

	for _, sp := range testPlayers {
		id := playerDocID(sp.Name, sp.Country)
		player := models.Player{
			ID:       id,
			Name:     sp.Name,
			Country:  sp.Country,
			Position: sp.Position,
			ClubTeam: sp.ClubTeam,
			Rating:   sp.Rating,
		}
		if _, err := client.Collection("players").Doc(id).Set(ctx, player); err != nil {
			fmt.Printf("  ✗ %-25s %v\n", sp.Name, err)
		} else {
			fmt.Printf("  ✓ %-25s %-12s %-3s  rating %d\n", sp.Name, sp.Country, sp.Position, sp.Rating)
		}
	}

	fmt.Printf("\nDone.\n")
}
