// cmd/sofifa-test checks how well our seed player names match against the EA FC26 CSV.
// It loads the CSV, then for each seed player tries to find a name match and reports
// exact, fuzzy, and missing matches along with the OVR rating for each hit.
//
// Run with: go run ./cmd/sofifa-test
package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"unicode"
)

const csvPath = "/Users/andrewmartin/Downloads/archive/EAFC26-Men.csv"

// Seed players — mirrors cmd/seed/main.go
type seedPlayer struct {
	Name     string
	Country  string
	Position string
}

var seedPlayers = []seedPlayer{
	{"Alisson", "Brazil", "GK"},
	{"Marc-Andre ter Stegen", "Germany", "GK"},
	{"Thibaut Courtois", "Belgium", "GK"},
	{"Jordan Pickford", "England", "GK"},
	{"Unai Simon", "Spain", "GK"},

	{"Virgil van Dijk", "Netherlands", "DEF"},
	{"Achraf Hakimi", "Morocco", "DEF"},
	{"Theo Hernandez", "France", "DEF"},
	{"Ruben Dias", "Portugal", "DEF"},
	{"Eder Militao", "Brazil", "DEF"},
	{"Joao Cancelo", "Portugal", "DEF"},
	{"Antonio Rudiger", "Germany", "DEF"},
	{"Marquinhos", "Brazil", "DEF"},
	{"Kyle Walker", "England", "DEF"},
	{"Jules Kounde", "France", "DEF"},
	{"Ronald Araujo", "Uruguay", "DEF"},

	{"Kevin De Bruyne", "Belgium", "MID"},
	{"Luka Modric", "Croatia", "MID"},
	{"Pedri", "Spain", "MID"},
	{"Jude Bellingham", "England", "MID"},
	{"Federico Valverde", "Uruguay", "MID"},
	{"Gavi", "Spain", "MID"},
	{"Bruno Fernandes", "Portugal", "MID"},
	{"Declan Rice", "England", "MID"},
	{"Alexis Mac Allister", "Argentina", "MID"},
	{"Bernardo Silva", "Portugal", "MID"},

	{"Kylian Mbappe", "France", "FWD"},
	{"Erling Haaland", "Norway", "FWD"},
	{"Vinicius Jr", "Brazil", "FWD"},
	{"Lamine Yamal", "Spain", "FWD"},
	{"Harry Kane", "England", "FWD"},
	{"Mohamed Salah", "Egypt", "FWD"},
	{"Antoine Griezmann", "France", "FWD"},
	{"Son Heung-min", "South Korea", "FWD"},
	{"Marcus Rashford", "England", "FWD"},
}

type csvPlayer struct {
	Name   string
	OVR    int
	Nation string
}

type matchResult struct {
	SeedName string
	Country  string
	Position string
	Matched  string
	Rating   int
	Quality  string // EXACT, FUZZY, MISS
}

func main() {
	// 1. Load CSV
	f, err := os.Open(csvPath)
	if err != nil {
		log.Fatalf("open csv: %v", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	headers, err := r.Read()
	if err != nil {
		log.Fatalf("read csv header: %v", err)
	}

	// Find column indices
	col := map[string]int{}
	for i, h := range headers {
		col[h] = i
	}
	nameCol, ovrCol, nationCol := col["Name"], col["OVR"], col["Nation"]

	var allPlayers []csvPlayer
	for {
		row, err := r.Read()
		if err != nil {
			break
		}
		ovr, _ := strconv.Atoi(row[ovrCol])
		allPlayers = append(allPlayers, csvPlayer{
			Name:   row[nameCol],
			OVR:    ovr,
			Nation: row[nationCol],
		})
	}
	fmt.Printf("Loaded %d players from CSV\n\n", len(allPlayers))

	// 2. Build index by nation for faster lookup
	byNation := map[string][]csvPlayer{}
	for _, p := range allPlayers {
		byNation[p.Nation] = append(byNation[p.Nation], p)
	}

	// 3. Match seed players
	results := make([]matchResult, 0, len(seedPlayers))
	for _, sp := range seedPlayers {
		// Try nation-scoped match first, then global fallback
		candidates := byNation[sp.Country]
		matched, rating, quality := findBestMatch(sp.Name, candidates)
		if quality == "MISS" {
			// fallback: search all players (handles nation name mismatches)
			matched, rating, quality = findBestMatch(sp.Name, allPlayers)
			if quality != "MISS" {
				quality = quality + "*" // mark as cross-nation match
			}
		}
		results = append(results, matchResult{
			SeedName: sp.Name,
			Country:  sp.Country,
			Position: sp.Position,
			Matched:  matched,
			Rating:   rating,
			Quality:  quality,
		})
	}

	// 4. Print results table
	fmt.Printf("%-30s %-14s %-5s %-30s %-6s %s\n",
		"Seed Name", "Country", "Pos", "FC26 Match", "OVR", "Quality")
	fmt.Println(strings.Repeat("─", 95))

	exact, fuzzy, miss := 0, 0, 0
	for _, r := range results {
		switch {
		case strings.HasPrefix(r.Quality, "EXACT"):
			exact++
		case strings.HasPrefix(r.Quality, "FUZZY"):
			fuzzy++
		default:
			miss++
		}
		ratingStr := fmt.Sprintf("%d", r.Rating)
		if r.Rating == 0 {
			ratingStr = "─"
		}
		fmt.Printf("%-30s %-14s %-5s %-30s %-6s %s\n",
			r.SeedName, r.Country, r.Position, r.Matched, ratingStr, r.Quality)
	}

	fmt.Println(strings.Repeat("─", 95))
	fmt.Printf("Results: %d EXACT  %d FUZZY  %d MISS  (total %d)\n",
		exact, fuzzy, miss, len(seedPlayers))
	fmt.Println("\n* = matched globally (nation name mismatch between seed and CSV)")
}

// findBestMatch tries to match a seed player name against a list of CSV players.
// Returns (matchedName, OVR, quality) where quality is EXACT, FUZZY, or MISS.
func findBestMatch(name string, players []csvPlayer) (string, int, string) {
	norm := normalizeName(name)

	// Pass 1: exact normalized match
	for _, p := range players {
		if normalizeName(p.Name) == norm {
			return p.Name, p.OVR, "EXACT"
		}
	}

	// Pass 2: all words in seed name appear in player name (handles accents/abbreviations)
	words := strings.Fields(norm)
	for _, p := range players {
		pNorm := normalizeName(p.Name)
		allMatch := true
		for _, w := range words {
			if !strings.Contains(pNorm, w) {
				allMatch = false
				break
			}
		}
		if allMatch {
			return p.Name, p.OVR, "FUZZY"
		}
	}

	// Pass 3: last word (surname) match within same nation
	if len(words) > 0 {
		lastName := words[len(words)-1]
		for _, p := range players {
			pNorm := normalizeName(p.Name)
			pWords := strings.Fields(pNorm)
			if len(pWords) > 0 && pWords[len(pWords)-1] == lastName {
				return p.Name, p.OVR, "FUZZY"
			}
		}
	}

	return "NOT FOUND", 0, "MISS"
}

// normalizeName lowercases, strips accents, and collapses spaces.
func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || r == ' ' {
			// strip combining/accent characters by only keeping basic latin
			if r <= 127 {
				b.WriteRune(r)
			} else {
				// map common accented chars to ascii equivalents
				b.WriteRune(deaccent(r))
			}
		} else if r == '-' || r == '\'' {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// deaccent maps accented runes to their base ASCII equivalent.
func deaccent(r rune) rune {
	switch r {
	case 'á', 'à', 'â', 'ä', 'ã', 'å':
		return 'a'
	case 'é', 'è', 'ê', 'ë':
		return 'e'
	case 'í', 'ì', 'î', 'ï':
		return 'i'
	case 'ó', 'ò', 'ô', 'ö', 'õ':
		return 'o'
	case 'ú', 'ù', 'û', 'ü':
		return 'u'
	case 'ñ':
		return 'n'
	case 'ç':
		return 'c'
	case 'ß':
		return 's'
	case 'ø':
		return 'o'
	case 'ý':
		return 'y'
	case 'ž', 'ź', 'ż':
		return 'z'
	case 'š', 'ś':
		return 's'
	case 'č', 'ć':
		return 'c'
	case 'ř':
		return 'r'
	case 'ð':
		return 'd'
	case 'þ':
		return 't'
	}
	return r
}

