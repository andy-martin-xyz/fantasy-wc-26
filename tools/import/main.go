// tools/import builds the WC 2026 player pool. This is a one-time/per-tournament
// bootstrap tool, not part of the deployed service — kept for the next World
// Cup rather than deleted, since resolving squads + ratings + ESPN ids from
// scratch is real work.
//
// It reads the announced squads (tools/import/data/*.tsv, one line per player:
// "Team\tPOS\tName\tClub") and enriches each player with an FIFA-style overall
// rating matched from the EA FC26 men's CSV. It maps Wikipedia positions
// (GK/DF/MF/FW) to the app's GK/DEF/MID/FWD, and emits a players.json payload
// shaped exactly like POST /api/admin/players/import.
//
// tools/espn-ids enriches this same players.json in place with ESPN athlete
// ids afterwards — run this first, then that.
//
// Usage:
//
//	go run ./tools/import                     # write tools/import/players.json
//	go run ./tools/import -firestore          # also upsert to Firestore (needs
//	                                          # FIRESTORE_EMULATOR_HOST, or
//	                                          # SEED_PRODUCTION=true for prod)
//
// Flags: -csv, -data, -out, -miss-default, -firestore.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"cloud.google.com/go/firestore"
	"github.com/andrewmartin/fantasy-league/internal/models"
	"golang.org/x/text/unicode/norm"
)

func main() {
	var (
		csvPath      = flag.String("csv", "/Users/andrewmartin/Downloads/archive/EAFC26-Men.csv", "path to EA FC26 men's CSV")
		dataDir      = flag.String("data", "tools/import/data", "directory of squad .tsv files")
		outPath      = flag.String("out", "tools/import/players.json", "output JSON path (import-endpoint shape)")
		overridePath = flag.String("overrides", "tools/import/overrides.tsv", "manual rating overrides (Country\\tName\\tRating)")
		missDefault  = flag.Int("miss-default", 66, "rating assigned to players with no CSV match (≈ real CSV median)")
		toFirestore  = flag.Bool("firestore", false, "also upsert players directly to Firestore")
		clearFirst   = flag.Bool("clear", false, "delete all existing players before upserting (use with -firestore)")
	)
	flag.Parse()

	squads, err := loadSquads(*dataDir)
	if err != nil {
		log.Fatalf("load squads: %v", err)
	}
	fmt.Printf("Loaded %d squad players from %s\n", len(squads), *dataDir)

	csvPlayers, err := loadCSV(*csvPath)
	if err != nil {
		log.Fatalf("load csv: %v", err)
	}
	fmt.Printf("Loaded %d rated players from CSV\n\n", len(csvPlayers))

	byNation := map[string][]csvPlayer{}
	for _, p := range csvPlayers {
		key := normalizeName(p.Nation)
		byNation[key] = append(byNation[key], p)
	}

	overrides := loadOverrides(*overridePath)
	if len(overrides) > 0 {
		fmt.Printf("Loaded %d manual rating overrides from %s\n\n", len(overrides), *overridePath)
	}

	var out []importPlayer
	var misses []squadPlayer
	exact, fuzzy, miss, overridden := 0, 0, 0, 0

	for _, sp := range squads {
		// Build nation-scoped candidate set, fall back to all players.
		var cands []csvPlayer
		for _, nk := range csvNationKeys(sp.Team) {
			cands = append(cands, byNation[nk]...)
		}
		scoped := len(cands) > 0
		if !scoped {
			cands = csvPlayers
		}

		matched, ovr, quality := findBestMatch(sp.Name, cands, scoped)
		if quality == "MISS" && scoped {
			matched, ovr, quality = findBestMatch(sp.Name, csvPlayers, false)
		}

		rating := ovr
		switch {
		case strings.HasPrefix(quality, "EXACT"):
			exact++
		case strings.HasPrefix(quality, "FUZZY"):
			fuzzy++
		default:
			rating = *missDefault
		}

		// Manual override wins over both CSV match and default.
		if r, ok := overrides[overrideKey(sp.Team, sp.Name)]; ok {
			rating = r
			overridden++
		} else if quality == "MISS" {
			miss++
			misses = append(misses, sp)
		}

		out = append(out, importPlayer{
			Name:     sp.Name,
			Country:  sp.Team,
			Position: mapPosition(sp.Pos),
			ClubTeam: sp.Club,
			Rating:   rating,
			ImageURL: matched.Image,
		})
	}

	fmt.Printf("Match results: %d EXACT  %d FUZZY  %d MISS  %d OVERRIDDEN  (total %d)\n",
		exact, fuzzy, miss, overridden, len(squads))
	fmt.Printf("Rated from real data: %.1f%% (CSV match + manual override)\n\n",
		100*float64(exact+fuzzy+overridden)/float64(len(squads)))

	writeMisses(misses, *missDefault)

	if err := writeJSON(*outPath, out); err != nil {
		log.Fatalf("write json: %v", err)
	}
	fmt.Printf("Wrote %d players → %s\n", len(out), *outPath)

	if *toFirestore {
		if err := upsertFirestore(out, *clearFirst); err != nil {
			log.Fatalf("firestore upsert: %v", err)
		}
	}
}

// --- squad loading ---------------------------------------------------------

type squadPlayer struct {
	Team string
	Pos  string // GK/DF/MF/FW
	Name string
	Club string
}

func loadSquads(dir string) ([]squadPlayer, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.tsv"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .tsv files in %s", dir)
	}
	sort.Strings(files)

	var players []squadPlayer
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for i, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			parts := strings.Split(line, "\t")
			if len(parts) != 4 {
				return nil, fmt.Errorf("%s line %d: expected 4 tab-separated fields, got %d", f, i+1, len(parts))
			}
			players = append(players, squadPlayer{
				Team: strings.TrimSpace(parts[0]),
				Pos:  strings.TrimSpace(parts[1]),
				Name: strings.TrimSpace(parts[2]),
				Club: strings.TrimSpace(parts[3]),
			})
		}
	}
	return players, nil
}

// --- CSV loading -----------------------------------------------------------

type csvPlayer struct {
	Name   string
	OVR    int
	Nation string
	Image  string
}

func loadCSV(path string) ([]csvPlayer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // tolerate ragged rows
	headers, err := r.Read()
	if err != nil {
		return nil, err
	}
	col := map[string]int{}
	for i, h := range headers {
		col[strings.TrimSpace(h)] = i
	}
	nameI, ovrI, natI := col["Name"], col["OVR"], col["Nation"]
	imgI, ok := col["card"] // last column holds the player's webp face image
	_ = ok

	get := func(row []string, i int) string {
		if i >= 0 && i < len(row) {
			return row[i]
		}
		return ""
	}

	var players []csvPlayer
	for {
		row, err := r.Read()
		if err != nil {
			break
		}
		ovr, _ := strconv.Atoi(get(row, ovrI))
		players = append(players, csvPlayer{
			Name:   get(row, nameI),
			OVR:    ovr,
			Nation: get(row, natI),
			Image:  get(row, imgI),
		})
	}
	return players, nil
}

// --- matching --------------------------------------------------------------

// findBestMatch returns the best CSV match for a squad name. surnameOK enables
// last-name-only matching, which is only safe inside a nation-scoped candidate
// set (otherwise it produces cross-nation false positives).
func findBestMatch(name string, players []csvPlayer, surnameOK bool) (csvPlayer, int, string) {
	norm := normalizeName(name)

	// Pass 1: exact normalized match.
	for _, p := range players {
		if normalizeName(p.Name) == norm {
			return p, p.OVR, "EXACT"
		}
	}

	// Pass 2: every word of the squad name appears in the CSV name.
	words := strings.Fields(norm)
	for _, p := range players {
		pn := normalizeName(p.Name)
		all := true
		for _, w := range words {
			if !strings.Contains(pn, w) {
				all = false
				break
			}
		}
		if all {
			return p, p.OVR, "FUZZY"
		}
	}

	// Pass 2.5: reverse containment, nation-scoped only. EA often lists a player
	// under a mononym ("Gabriel", "Brahim") shorter than the squad's full name.
	// Accept only when exactly one candidate's whole name is a subset of the
	// squad name, so ambiguous shared names (two "Ederson"s) are left unmatched.
	if surnameOK {
		var hits []csvPlayer
		for _, p := range players {
			pw := strings.Fields(normalizeName(p.Name))
			if len(pw) == 0 {
				continue
			}
			subset := true
			for _, w := range pw {
				if !containsWord(words, w) {
					subset = false
					break
				}
			}
			if subset {
				hits = append(hits, p)
			}
		}
		if len(hits) == 1 {
			return hits[0], hits[0].OVR, "FUZZY"
		}
	}

	// Pass 3: surname (last token), nation-scoped and unique only. Requiring a
	// single candidate avoids false positives from shared suffixes/surnames
	// ("Júnior", "Silva", "Santos") that several players share.
	if surnameOK && len(words) > 0 {
		last := words[len(words)-1]
		var hits []csvPlayer
		for _, p := range players {
			pw := strings.Fields(normalizeName(p.Name))
			if len(pw) > 0 && pw[len(pw)-1] == last {
				hits = append(hits, p)
			}
		}
		if len(hits) == 1 {
			return hits[0], hits[0].OVR, "FUZZY"
		}
	}

	return csvPlayer{}, 0, "MISS"
}

// csvNationKeys maps an app team name to the normalized nation spelling(s) the
// CSV is likely to use. Returns normalized keys to look up in the nation index.
func csvNationKeys(team string) []string {
	keys := []string{normalizeName(team)}
	for _, alt := range nationAliases[team] {
		keys = append(keys, normalizeName(alt))
	}
	return keys
}

var nationAliases = map[string][]string{
	"Netherlands":            {"Holland"},
	"South Korea":            {"Korea Republic"},
	"Ivory Coast":            {"Cote d'Ivoire", "Côte d'Ivoire"},
	"Turkey":                 {"Türkiye", "Turkiye"},
	"Czech Republic":         {"Czechia"},
	"DR Congo":               {"Congo DR", "Congo"},
	"Bosnia and Herzegovina": {"Bosnia Herzegovina"},
	"Cape Verde":             {"Cabo Verde"},
}

// normalizeName lowercases, strips accents and punctuation, and collapses
// whitespace so names compare across spelling/diacritic differences.
func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// Replace separators with spaces before decomposition.
	s = strings.NewReplacer("-", " ", "'", " ", "’", " ", ".", " ", "­", "", "​", "").Replace(s)
	// Map standalone letters NFD won't decompose.
	s = strings.NewReplacer(
		"ø", "o", "đ", "d", "ð", "d", "ł", "l", "ß", "ss",
		"þ", "th", "ı", "i", "æ", "ae", "œ", "oe", "ŋ", "n",
	).Replace(s)
	// Decompose and drop combining marks (accents).
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

// loadOverrides reads "Country\tName\tRating" lines (blank lines and #-comments
// ignored). Missing file is fine — returns an empty map.
func loadOverrides(path string) map[string]int {
	m := map[string]int{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			fmt.Printf("warning: skipping malformed override line: %q\n", line)
			continue
		}
		rating, err := strconv.Atoi(strings.TrimSpace(parts[2]))
		if err != nil {
			fmt.Printf("warning: bad rating in override line: %q\n", line)
			continue
		}
		m[overrideKey(parts[0], parts[1])] = rating
	}
	return m
}

func overrideKey(country, name string) string {
	return normalizeName(country) + "|" + normalizeName(name)
}

func containsWord(words []string, w string) bool {
	for _, x := range words {
		if x == w {
			return true
		}
	}
	return false
}

func mapPosition(p string) string {
	switch strings.ToUpper(strings.TrimSpace(p)) {
	case "GK":
		return "GK"
	case "DF":
		return "DEF"
	case "MF":
		return "MID"
	case "FW":
		return "FWD"
	}
	return strings.ToUpper(p)
}

// --- output ----------------------------------------------------------------

type importPlayer struct {
	Name     string `json:"name"`
	Country  string `json:"country"`
	Position string `json:"position"`
	ClubTeam string `json:"clubTeam"`
	Rating   int    `json:"rating"`
	ImageURL string `json:"imageURL"`
}

func writeJSON(path string, players []importPlayer) error {
	payload := map[string]any{"players": players}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeMisses(misses []squadPlayer, def int) {
	if len(misses) == 0 {
		fmt.Println("No unmatched players — every squad player got a real rating.")
		return
	}
	const path = "tools/import/misses.txt"
	var b strings.Builder
	fmt.Fprintf(&b, "# %d players had no CSV rating match (assigned default %d)\n", len(misses), def)
	fmt.Fprintf(&b, "# Team\tPOS\tName\tClub\n")
	for _, m := range misses {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", m.Team, m.Pos, m.Name, m.Club)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fmt.Printf("warning: could not write misses report: %v\n", err)
		return
	}
	fmt.Printf("Wrote %d unmatched players → %s (assigned default rating %d)\n", len(misses), path, def)
}

// --- firestore -------------------------------------------------------------

func upsertFirestore(players []importPlayer, clearFirst bool) error {
	emulator := os.Getenv("FIRESTORE_EMULATOR_HOST") != ""
	production := os.Getenv("SEED_PRODUCTION") == "true"
	if !emulator && !production {
		return fmt.Errorf("set FIRESTORE_EMULATOR_HOST for the emulator, or SEED_PRODUCTION=true to write production")
	}
	if production && !emulator {
		fmt.Println("WARNING: writing to PRODUCTION Firestore")
	}

	projectID := os.Getenv("FIREBASE_PROJECT_ID")
	if projectID == "" {
		projectID = "andy-personal-1bb38"
	}

	ctx := context.Background()
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return err
	}
	defer client.Close()

	if clearFirst {
		col := client.Collection("players")
		deleted := 0
		for {
			docs, err := col.Limit(400).Documents(ctx).GetAll()
			if err != nil {
				return fmt.Errorf("list players for clear: %w", err)
			}
			if len(docs) == 0 {
				break
			}
			batch := client.BulkWriter(ctx)
			for _, d := range docs {
				if _, err := batch.Delete(d.Ref); err != nil {
					return fmt.Errorf("queue delete %s: %w", d.Ref.ID, err)
				}
			}
			batch.End()
			deleted += len(docs)
		}
		fmt.Printf("Cleared %d existing players.\n", deleted)
	}

	fmt.Printf("\nUpserting %d players into project %q...\n", len(players), projectID)
	for _, p := range players {
		id := playerDocID(p.Name, p.Country)
		player := models.Player{
			ID:       id,
			Name:     p.Name,
			Country:  p.Country,
			Position: p.Position,
			ClubTeam: p.ClubTeam,
			Rating:   p.Rating,
			ImageURL: p.ImageURL,
		}
		if _, err := client.Collection("players").Doc(id).Set(ctx, player); err != nil {
			fmt.Printf("  ✗ %-28s %v\n", p.Name, err)
		}
	}
	fmt.Println("Done.")
	return nil
}

// playerDocID mirrors internal/handlers/players.go.
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
