// tools/espn-ids resolves an ESPN athlete id for every player in the pool so
// that, once the World Cup starts, live match stats are matched to drafted
// players by id instead of by name. The name matching happens once, here —
// never per match. Run after tools/import.
//
// It reads the pool (tools/import/players.json), fetches each nation's ESPN
// roster, matches within each nation, and enriches the same file in place
// with espnAthleteId (plus a misses report) — one canonical player-pool file
// instead of a separate output per enrichment step. With -firestore it also
// writes espnAthleteId onto each player doc.
//
// Usage:
//
//	go run ./tools/espn-ids                 # report coverage, write JSON (no DB writes)
//	go run ./tools/espn-ids -only Brazil    # probe one nation
//	SEED_PRODUCTION=true go run ./tools/espn-ids -firestore   # also write ids to prod
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"unicode"

	"cloud.google.com/go/firestore"
	"golang.org/x/text/unicode/norm"

	"github.com/andrewmartin/fantasy-league/internal/espn"
	"github.com/andrewmartin/fantasy-league/internal/models"
)

func main() {
	var (
		inPath       = flag.String("in", "tools/import/players.json", "input player pool (import-endpoint shape)")
		outPath      = flag.String("out", "tools/import/players.json", "output JSON path — same file as -in by default, enriched in place")
		only         = flag.String("only", "", "process a single nation only (exact app name, e.g. \"Brazil\")")
		overridePath = flag.String("overrides", "tools/espn-ids/espn-overrides.tsv", "manual Country\\tName\\tespnId overrides")
		toFirestore  = flag.Bool("firestore", false, "write espnAthleteId onto each matched player doc")
	)
	flag.Parse()
	ctx := context.Background()

	pool, err := loadPool(*inPath)
	if err != nil {
		log.Fatalf("load pool: %v", err)
	}
	overrides := loadOverrides(*overridePath)

	// Resolve ESPN team ids and map each to our canonical country name.
	teams, err := espn.FetchTeams(ctx)
	if err != nil {
		log.Fatalf("fetch espn teams: %v", err)
	}
	espnTeamID := map[string]string{} // our country -> espn team id
	var unmappedTeams []string
	for _, t := range teams {
		if c := ourCountry(t.Name); c != "" {
			espnTeamID[c] = t.ID
		} else {
			unmappedTeams = append(unmappedTeams, t.Name)
		}
	}
	if len(unmappedTeams) > 0 {
		fmt.Printf("⚠ ESPN teams not mapped to a pool nation (add an alias): %v\n\n", unmappedTeams)
	}

	// Group pool players by country.
	byCountry := map[string][]*poolPlayer{}
	countries := []string{}
	for i := range pool {
		p := &pool[i]
		if _, seen := byCountry[p.Country]; !seen {
			countries = append(countries, p.Country)
		}
		byCountry[p.Country] = append(byCountry[p.Country], p)
	}
	sort.Strings(countries)

	matched, missed, noTeam := 0, 0, 0
	var misses []string

	for _, country := range countries {
		if *only != "" && country != *only {
			continue
		}
		teamID, ok := espnTeamID[country]
		if !ok {
			noTeam += len(byCountry[country])
			for _, p := range byCountry[country] {
				misses = append(misses, fmt.Sprintf("%s\t%s\t(no ESPN team)", country, p.Name))
			}
			continue
		}
		roster, err := espn.FetchRoster(ctx, teamID)
		if err != nil {
			log.Printf("fetch roster %s (%s): %v", country, teamID, err)
			continue
		}
		cands := make([]espnCand, len(roster))
		for i, a := range roster {
			cands[i] = espnCand{ID: a.ID, norm: normName(a.Name), words: strings.Fields(normName(a.Name))}
		}
		for _, p := range byCountry[country] {
			if id, ok := overrides[overrideKey(country, p.Name)]; ok {
				p.ESPNAthleteID = id
				matched++
				continue
			}
			if id := matchID(p.Name, cands); id != "" {
				p.ESPNAthleteID = id
				matched++
			} else {
				missed++
				misses = append(misses, fmt.Sprintf("%s\t%s\t(no roster match)", country, p.Name))
			}
		}
	}

	total := matched + missed + noTeam
	fmt.Printf("Matched %d / %d players to an ESPN id (%.1f%%)\n", matched, total, 100*float64(matched)/float64(max(total, 1)))
	fmt.Printf("  %d no roster-name match, %d in a nation with no ESPN team\n", missed, noTeam)

	writeMisses(misses)
	if err := writeJSON(*outPath, pool); err != nil {
		log.Fatalf("write json: %v", err)
	}
	fmt.Printf("Wrote %s\n", *outPath)

	if *toFirestore {
		if err := writeFirestore(ctx, pool); err != nil {
			log.Fatalf("firestore: %v", err)
		}
	}
}

// --- pool ------------------------------------------------------------------

type poolPlayer struct {
	Name          string `json:"name"`
	Country       string `json:"country"`
	Position      string `json:"position"`
	ClubTeam      string `json:"clubTeam"`
	Rating        int    `json:"rating"`
	ImageURL      string `json:"imageURL"`
	ESPNAthleteID string `json:"espnAthleteId,omitempty"`
}

func loadPool(path string) ([]poolPlayer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Players []poolPlayer `json:"players"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		return nil, err
	}
	return wrap.Players, nil
}

func writeJSON(path string, pool []poolPlayer) error {
	data, err := json.MarshalIndent(map[string]any{"players": pool}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeMisses(misses []string) {
	if len(misses) == 0 {
		fmt.Println("No misses — every player resolved to an ESPN id.")
		return
	}
	const path = "tools/espn-ids/misses.txt"
	body := "# country\tname\treason\n" + strings.Join(misses, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		fmt.Printf("warning: could not write misses: %v\n", err)
		return
	}
	fmt.Printf("Wrote %d unmatched players → %s\n", len(misses), path)
}

// --- ESPN team name → our country -----------------------------------------

// espnTeamAliases covers ESPN spellings that NormalizeTeamName doesn't already
// resolve. Populated as the team-list report surfaces unmapped names.
var espnTeamAliases = map[string]string{
	"bosnia-herzegovina": "Bosnia and Herzegovina",
}

func loadOverrides(path string) map[string]string {
	m := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			continue
		}
		m[overrideKey(parts[0], parts[1])] = strings.TrimSpace(parts[2])
	}
	return m
}

func overrideKey(country, name string) string {
	return normName(country) + "|" + normName(name)
}

func ourCountry(espnName string) string {
	if c := models.NormalizeTeamName(espnName); c != "" {
		return c
	}
	return espnTeamAliases[strings.ToLower(strings.TrimSpace(espnName))]
}

// --- matching --------------------------------------------------------------

type espnCand struct {
	ID    string
	norm  string
	words []string
}

func matchID(name string, cands []espnCand) string {
	n := normName(name)
	if n == "" {
		return ""
	}
	for _, c := range cands { // exact
		if c.norm == n {
			return c.ID
		}
	}
	words := strings.Fields(n)
	var hits []espnCand
	for _, c := range cands { // token subset either direction, unique
		if subset(words, c.words) || subset(c.words, words) {
			hits = append(hits, c)
		}
	}
	if len(hits) == 1 {
		return hits[0].ID
	}
	if len(words) > 0 { // unique surname
		last := words[len(words)-1]
		hits = hits[:0]
		for _, c := range cands {
			if len(c.words) > 0 && c.words[len(c.words)-1] == last {
				hits = append(hits, c)
			}
		}
		if len(hits) == 1 {
			return hits[0].ID
		}
	}
	return ""
}

func subset(a, b []string) bool {
	if len(a) == 0 {
		return false
	}
	for _, w := range a {
		found := false
		for _, x := range b {
			if x == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func normName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("-", " ", "'", " ", "’", " ", ".", " ").Replace(s)
	s = strings.NewReplacer("ø", "o", "đ", "d", "ð", "d", "ł", "l", "ß", "ss",
		"þ", "th", "ı", "i", "æ", "ae", "œ", "oe").Replace(s)
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// --- firestore -------------------------------------------------------------

func writeFirestore(ctx context.Context, pool []poolPlayer) error {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" && os.Getenv("SEED_PRODUCTION") != "true" {
		return fmt.Errorf("set FIRESTORE_EMULATOR_HOST or SEED_PRODUCTION=true to write")
	}
	projectID := os.Getenv("FIREBASE_PROJECT_ID")
	if projectID == "" {
		projectID = "andy-personal-1bb38"
	}
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return err
	}
	defer client.Close()

	n := 0
	for _, p := range pool {
		if p.ESPNAthleteID == "" {
			continue
		}
		id := playerDocID(p.Name, p.Country)
		if _, err := client.Collection("players").Doc(id).Update(ctx, []firestore.Update{
			{Path: "espnAthleteId", Value: p.ESPNAthleteID},
		}); err != nil {
			fmt.Printf("  ✗ %-26s %v\n", p.Name, err)
			continue
		}
		n++
	}
	fmt.Printf("Wrote espnAthleteId onto %d player docs in %q.\n", n, projectID)
	return nil
}

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
		return strings.Trim(strings.Join(strings.Fields(string(out)), "-"), "-")
	}
	return slug(name) + "_" + slug(country)
}
