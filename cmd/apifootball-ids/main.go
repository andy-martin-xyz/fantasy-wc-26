// cmd/apifootball-ids resolves an API-Football player ID for every player in the
// curated pool (cmd/import/players.json) so that, once the World Cup kicks off,
// live match stats can be matched by ID instead of by name. The name matching
// happens once, here — never again per match.
//
// Strategy (API-Football free tier = 100 requests/day):
//
//	1. Resolve each nation -> API-Football national-team ID   (~48 requests)
//	2. Pull each nation's squad via /players/squads?team={id} (48 requests)
//	3. Match our players to squad entries WITHIN each nation   (0 extra requests)
//
// Every HTTP response is cached to cmd/apifootball-ids/cache/, so re-runs cost
// zero quota. Resolved team IDs are cached to teams.json.
//
// Setup: put your key in .env (or the environment) as API_FOOTBALL_KEY. The
// default provider is api-sports.io (direct); pass -rapidapi if your key came
// from RapidAPI instead.
//
// Usage:
//
//	go run ./cmd/apifootball-ids                  # all nations -> players-with-ids.json + misses.txt
//	go run ./cmd/apifootball-ids -only Brazil     # one nation, to probe coverage cheaply first
//	go run ./cmd/apifootball-ids -limit 10        # cap live requests this run (quota safety)
//	go run ./cmd/apifootball-ids -write-back      # also write apiFootballId into cmd/import/players.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

func main() {
	var (
		inPath    = flag.String("in", "cmd/import/players.json", "input player pool (import-endpoint shape)")
		outPath   = flag.String("out", "cmd/apifootball-ids/players-with-ids.json", "output JSON, pool + apiFootballId")
		cacheDir  = flag.String("cache", "cmd/apifootball-ids/cache", "directory for cached API responses")
		teamsPath = flag.String("teams", "cmd/apifootball-ids/teams.json", "nation -> team ID cache")
		envPath   = flag.String("env", ".env", "dotenv file to read API_FOOTBALL_KEY from")
		only      = flag.String("only", "", "process a single nation only (exact app name, e.g. \"Brazil\")")
		limit     = flag.Int("limit", 0, "max LIVE (uncached) requests this run; 0 = unlimited")
		delay     = flag.Duration("delay", 7*time.Second, "pause between live requests (stay under per-minute caps)")
		rapidAPI  = flag.Bool("rapidapi", false, "key is from RapidAPI (default: api-sports.io direct)")
		writeBack = flag.Bool("write-back", false, "also write apiFootballId into the -in file in place")
	)
	flag.Parse()

	loadDotEnv(*envPath)
	key := os.Getenv("API_FOOTBALL_KEY")
	if key == "" {
		log.Fatal("API_FOOTBALL_KEY not set — add it to .env or the environment.\n" +
			"Get a free key at dashboard.api-football.com or via RapidAPI (pass -rapidapi).")
	}

	pool, err := loadPool(*inPath)
	if err != nil {
		log.Fatalf("load pool: %v", err)
	}
	fmt.Printf("Loaded %d players from %s\n", len(pool), *inPath)

	client := &apiClient{
		key:      key,
		rapidAPI: *rapidAPI,
		cacheDir: *cacheDir,
		delay:    *delay,
		limit:    *limit,
	}
	if err := os.MkdirAll(*cacheDir, 0o755); err != nil {
		log.Fatalf("create cache dir: %v", err)
	}

	// Group our pool by nation, preserving order.
	nations := []string{}
	byNation := map[string][]*poolPlayer{}
	for i := range pool {
		c := pool[i].Country
		if _, seen := byNation[c]; !seen {
			nations = append(nations, c)
		}
		byNation[c] = append(byNation[c], &pool[i])
	}
	if *only != "" {
		if _, ok := byNation[*only]; !ok {
			log.Fatalf("-only %q: no players with that country in the pool", *only)
		}
		nations = []string{*only}
	}

	teamCache := loadTeamCache(*teamsPath)

	var unresolved []string
	matched, total := 0, 0

	for _, nation := range nations {
		players := byNation[nation]
		total += len(players)

		teamID, ok := teamCache[nation]
		if !ok {
			id, err := client.resolveTeamID(nation)
			if err != nil {
				fmt.Printf("  ! %-24s could not resolve team ID: %v\n", nation, err)
				unresolved = append(unresolved, nation)
				continue
			}
			teamID = id
			teamCache[nation] = id
			saveTeamCache(*teamsPath, teamCache) // persist incrementally
		}

		squad, err := client.fetchSquad(teamID)
		if err != nil {
			fmt.Printf("  ! %-24s squad fetch failed (team %d): %v\n", nation, teamID, err)
			unresolved = append(unresolved, nation)
			continue
		}

		hit := 0
		for _, p := range players {
			id, sqName, quality := matchSquad(p.Name, squad)
			if quality != "MISS" {
				p.APIFootballID = id
				p.matchName = sqName
				p.matchQuality = quality
				hit++
				matched++
			}
		}
		fmt.Printf("  ✓ %-24s team %-6d squad %-3d  matched %d/%d\n",
			nation, teamID, len(squad), hit, len(players))
	}

	fmt.Printf("\nMatched %d/%d players (%.1f%%)\n", matched, total, pct(matched, total))
	if len(unresolved) > 0 {
		fmt.Printf("Unresolved nations (%d): %s\n", len(unresolved), strings.Join(unresolved, ", "))
		fmt.Println("  → add an alias in teamNameOverride, or pre-seed teams.json with the right ID.")
	}

	writeMisses(pool, *only)
	writeMatches(pool, *only)

	if err := writePool(*outPath, pool); err != nil {
		log.Fatalf("write out: %v", err)
	}
	fmt.Printf("Wrote %s\n", *outPath)

	if *writeBack {
		if err := writePool(*inPath, pool); err != nil {
			log.Fatalf("write back: %v", err)
		}
		fmt.Printf("Wrote apiFootballId back into %s\n", *inPath)
	}
}

// --- player pool I/O -------------------------------------------------------

// poolPlayer is the import-endpoint shape plus the resolved apiFootballId.
// matchName/matchQuality are run-local (not serialized) and feed the report.
type poolPlayer struct {
	Name          string `json:"name"`
	Country       string `json:"country"`
	Position      string `json:"position"`
	ClubTeam      string `json:"clubTeam"`
	Rating        int    `json:"rating"`
	ImageURL      string `json:"imageURL"`
	APIFootballID int    `json:"apiFootballId,omitempty"`

	matchName    string `json:"-"`
	matchQuality string `json:"-"`
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

func writePool(path string, players []poolPlayer) error {
	payload := map[string]any{"players": players}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeMisses(pool []poolPlayer, only string) {
	var b strings.Builder
	n := 0
	fmt.Fprintf(&b, "# Players with no API-Football ID match.\n")
	fmt.Fprintf(&b, "# Country\tPOS\tName\tClub\n")
	for _, p := range pool {
		if only != "" && p.Country != only {
			continue
		}
		if p.APIFootballID == 0 {
			fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", p.Country, p.Position, p.Name, p.ClubTeam)
			n++
		}
	}
	const path = "cmd/apifootball-ids/misses.txt"
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fmt.Printf("warning: could not write misses report: %v\n", err)
		return
	}
	fmt.Printf("Wrote %d unmatched players → %s\n", n, path)
}

// --- API-Football client ---------------------------------------------------

type apiClient struct {
	key      string
	rapidAPI bool
	cacheDir string
	delay    time.Duration
	limit    int
	made     int // live requests issued this run
}

func (c *apiClient) baseURL() string {
	if c.rapidAPI {
		return "https://api-football-v1.p.rapidapi.com/v3"
	}
	return "https://v3.football.api-sports.io"
}

// get issues a cached GET. The cache key is the path+query, so identical
// requests never spend quota twice. Respects -limit and -delay for live calls.
func (c *apiClient) get(path string, params url.Values) ([]byte, error) {
	cacheKey := path + "?" + params.Encode()
	cachePath := filepath.Join(c.cacheDir, cacheFileName(cacheKey))
	if data, err := os.ReadFile(cachePath); err == nil {
		return data, nil
	}

	if c.limit > 0 && c.made >= c.limit {
		return nil, fmt.Errorf("hit -limit=%d live requests; re-run to continue (cache persists)", c.limit)
	}
	if c.made > 0 {
		time.Sleep(c.delay)
	}

	u := c.baseURL() + path + "?" + params.Encode()
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if c.rapidAPI {
		req.Header.Set("x-rapidapi-key", c.key)
		req.Header.Set("x-rapidapi-host", "api-football-v1.p.rapidapi.com")
	} else {
		req.Header.Set("x-apisports-key", c.key)
	}

	c.made++
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(body))
	}
	// API-Football returns errors with HTTP 200 in an "errors" object.
	if err := checkAPIErrors(body); err != nil {
		return nil, err
	}

	_ = os.WriteFile(cachePath, body, 0o644)
	return body, nil
}

// resolveTeamID finds the national-team ID for an app nation name.
func (c *apiClient) resolveTeamID(nation string) (int, error) {
	search := nation
	if alt, ok := teamNameOverride[nation]; ok {
		search = alt
	}
	params := url.Values{}
	params.Set("search", search)
	body, err := c.get("/teams", params)
	if err != nil {
		return 0, err
	}
	var out struct {
		Response []struct {
			Team struct {
				ID       int    `json:"id"`
				Name     string `json:"name"`
				National bool   `json:"national"`
			} `json:"team"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, err
	}
	want := normalizeName(search)
	var firstNational int
	for _, r := range out.Response {
		if !r.Team.National {
			continue
		}
		if firstNational == 0 {
			firstNational = r.Team.ID
		}
		if normalizeName(r.Team.Name) == want {
			return r.Team.ID, nil // exact-name national team — best
		}
	}
	if firstNational != 0 {
		return firstNational, nil
	}
	return 0, fmt.Errorf("no national team in %d result(s) for %q", len(out.Response), search)
}

type squadMember struct {
	ID   int
	Name string
}

func (c *apiClient) fetchSquad(teamID int) ([]squadMember, error) {
	params := url.Values{}
	params.Set("team", fmt.Sprintf("%d", teamID))
	body, err := c.get("/players/squads", params)
	if err != nil {
		return nil, err
	}
	var out struct {
		Response []struct {
			Players []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"players"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if len(out.Response) == 0 {
		return nil, fmt.Errorf("empty squad response (coverage may not be live yet)")
	}
	var squad []squadMember
	for _, p := range out.Response[0].Players {
		squad = append(squad, squadMember{ID: p.ID, Name: p.Name})
	}
	return squad, nil
}

func checkAPIErrors(body []byte) error {
	// "errors" is [] when empty but an object {"key":"msg"} when populated.
	var probe struct {
		Errors json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil // not the shape we expect; let the caller's decode handle it
	}
	s := strings.TrimSpace(string(probe.Errors))
	if s == "" || s == "[]" || s == "null" {
		return nil
	}
	return fmt.Errorf("API error: %s", s)
}

// --- name matching (mirrors cmd/import's nation-scoped ladder) -------------

// matchSquad finds the best squad member for one of our player names. Because
// candidates are already one nation's squad, surname/subset passes are safe.
func matchSquad(name string, squad []squadMember) (int, string, string) {
	target := normalizeName(name)
	words := strings.Fields(target)

	// Pass 1: exact normalized match.
	for _, m := range squad {
		if normalizeName(m.Name) == target {
			return m.ID, m.Name, "EXACT"
		}
	}
	// Pass 2: every word of our name appears in the squad name.
	for _, m := range squad {
		mn := normalizeName(m.Name)
		all := true
		for _, w := range words {
			if !strings.Contains(mn, w) {
				all = false
				break
			}
		}
		if all && len(words) > 0 {
			return m.ID, m.Name, "FUZZY"
		}
	}
	// Pass 3: reverse containment — squad name is a subset of ours (mononyms).
	// Accept only if unique.
	var subsetHits []squadMember
	for _, m := range squad {
		mw := strings.Fields(normalizeName(m.Name))
		if len(mw) == 0 {
			continue
		}
		subset := true
		for _, w := range mw {
			if !containsWord(words, w) {
				subset = false
				break
			}
		}
		if subset {
			subsetHits = append(subsetHits, m)
		}
	}
	if len(subsetHits) == 1 {
		return subsetHits[0].ID, subsetHits[0].Name, "FUZZY"
	}
	// Pass 4: shared surname (last token), unique only.
	if len(words) > 0 {
		last := words[len(words)-1]
		var hits []squadMember
		for _, m := range squad {
			mw := strings.Fields(normalizeName(m.Name))
			if len(mw) > 0 && mw[len(mw)-1] == last {
				hits = append(hits, m)
			}
		}
		if len(hits) == 1 {
			return hits[0].ID, hits[0].Name, "FUZZY"
		}
	}
	return 0, "", "MISS"
}

func containsWord(words []string, w string) bool {
	for _, x := range words {
		if x == w {
			return true
		}
	}
	return false
}

// normalizeName mirrors cmd/import: lowercase, strip accents/punctuation,
// collapse whitespace, so names compare across spelling/diacritic differences.
func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("-", " ", "'", " ", "’", " ", ".", " ", "­", "", "​", "").Replace(s)
	s = strings.NewReplacer(
		"ø", "o", "đ", "d", "ð", "d", "ł", "l", "ß", "ss",
		"þ", "th", "ı", "i", "æ", "ae", "œ", "oe", "ŋ", "n",
	).Replace(s)
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

// teamNameOverride maps app nation names to API-Football's spelling where they
// differ. Unresolved nations are reported so this can be extended.
var teamNameOverride = map[string]string{
	"United States":          "USA",
	"Czech Republic":         "Czechia",
	"DR Congo":               "Congo DR",
	"Cape Verde":             "Cape Verde Islands",
	"Ivory Coast":            "Ivory Coast",
	"South Korea":            "South Korea",
	"Bosnia and Herzegovina": "Bosnia",
	"Curaçao":                "Curacao",
}

// --- caches & helpers ------------------------------------------------------

func loadTeamCache(path string) map[string]int {
	m := map[string]int{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

func saveTeamCache(path string, m map[string]int) {
	data, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(path, data, 0o644)
}

func cacheFileName(key string) string {
	var b strings.Builder
	for _, r := range key {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	name := b.String()
	if len(name) > 180 {
		name = name[:180]
	}
	return name + ".json"
}

func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			_ = os.Setenv(k, v)
		}
	}
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return 100 * float64(n) / float64(d)
}

// writeMatches dumps every resolved match for review. FUZZY rows are the ones
// worth eyeballing for false positives (our name -> the squad name we matched).
func writeMatches(pool []poolPlayer, only string) {
	rows := make([]poolPlayer, 0, len(pool))
	for _, p := range pool {
		if only != "" && p.Country != only {
			continue
		}
		if p.APIFootballID != 0 {
			rows = append(rows, p)
		}
	}
	// FUZZY first (needs review), then alphabetical by nation.
	sort.SliceStable(rows, func(i, j int) bool {
		if (rows[i].matchQuality == "FUZZY") != (rows[j].matchQuality == "FUZZY") {
			return rows[i].matchQuality == "FUZZY"
		}
		return rows[i].Country < rows[j].Country
	})
	var b strings.Builder
	fmt.Fprintf(&b, "# Resolved API-Football matches — review FUZZY rows for false positives.\n")
	fmt.Fprintf(&b, "# Quality\tCountry\tOurName\t->\tSquadName\tID\n")
	for _, p := range rows {
		fmt.Fprintf(&b, "%s\t%s\t%s\t->\t%s\t%d\n",
			p.matchQuality, p.Country, p.Name, p.matchName, p.APIFootballID)
	}
	const path = "cmd/apifootball-ids/matches.tsv"
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fmt.Printf("warning: could not write matches report: %v\n", err)
		return
	}
	fmt.Printf("Wrote %d matches → %s (review FUZZY rows)\n", len(rows), path)
}
