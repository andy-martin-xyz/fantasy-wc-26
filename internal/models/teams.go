package models

import "strings"

// Team represents a FIFA World Cup 2026 participating nation.
type Team struct {
	Name          string `json:"name"`
	Flag          string `json:"flag"`
	Confederation string `json:"confederation"` // UEFA CONMEBOL CONCACAF CAF AFC OFC
	Group         string `json:"group"`         // A–L
}

// WC2026Teams lists the 48 qualified nations for FIFA World Cup 2026, in group
// order (A–L). This mirrors the official squads list and matches the Country
// values used by the imported player pool (see cmd/import).
var WC2026Teams = []Team{
	// Group A
	{Name: "Czech Republic", Flag: "🇨🇿", Confederation: "UEFA", Group: "A"},
	{Name: "Mexico", Flag: "🇲🇽", Confederation: "CONCACAF", Group: "A"},
	{Name: "South Africa", Flag: "🇿🇦", Confederation: "CAF", Group: "A"},
	{Name: "South Korea", Flag: "🇰🇷", Confederation: "AFC", Group: "A"},

	// Group B
	{Name: "Bosnia and Herzegovina", Flag: "🇧🇦", Confederation: "UEFA", Group: "B"},
	{Name: "Canada", Flag: "🇨🇦", Confederation: "CONCACAF", Group: "B"},
	{Name: "Qatar", Flag: "🇶🇦", Confederation: "AFC", Group: "B"},
	{Name: "Switzerland", Flag: "🇨🇭", Confederation: "UEFA", Group: "B"},

	// Group C
	{Name: "Brazil", Flag: "🇧🇷", Confederation: "CONMEBOL", Group: "C"},
	{Name: "Haiti", Flag: "🇭🇹", Confederation: "CONCACAF", Group: "C"},
	{Name: "Morocco", Flag: "🇲🇦", Confederation: "CAF", Group: "C"},
	{Name: "Scotland", Flag: "🏴󠁧󠁢󠁳󠁣󠁴󠁿", Confederation: "UEFA", Group: "C"},

	// Group D
	{Name: "Australia", Flag: "🇦🇺", Confederation: "AFC", Group: "D"},
	{Name: "Paraguay", Flag: "🇵🇾", Confederation: "CONMEBOL", Group: "D"},
	{Name: "Turkey", Flag: "🇹🇷", Confederation: "UEFA", Group: "D"},
	{Name: "United States", Flag: "🇺🇸", Confederation: "CONCACAF", Group: "D"},

	// Group E
	{Name: "Curaçao", Flag: "🇨🇼", Confederation: "CONCACAF", Group: "E"},
	{Name: "Ecuador", Flag: "🇪🇨", Confederation: "CONMEBOL", Group: "E"},
	{Name: "Germany", Flag: "🇩🇪", Confederation: "UEFA", Group: "E"},
	{Name: "Ivory Coast", Flag: "🇨🇮", Confederation: "CAF", Group: "E"},

	// Group F
	{Name: "Japan", Flag: "🇯🇵", Confederation: "AFC", Group: "F"},
	{Name: "Netherlands", Flag: "🇳🇱", Confederation: "UEFA", Group: "F"},
	{Name: "Sweden", Flag: "🇸🇪", Confederation: "UEFA", Group: "F"},
	{Name: "Tunisia", Flag: "🇹🇳", Confederation: "CAF", Group: "F"},

	// Group G
	{Name: "Belgium", Flag: "🇧🇪", Confederation: "UEFA", Group: "G"},
	{Name: "Egypt", Flag: "🇪🇬", Confederation: "CAF", Group: "G"},
	{Name: "Iran", Flag: "🇮🇷", Confederation: "AFC", Group: "G"},
	{Name: "New Zealand", Flag: "🇳🇿", Confederation: "OFC", Group: "G"},

	// Group H
	{Name: "Cape Verde", Flag: "🇨🇻", Confederation: "CAF", Group: "H"},
	{Name: "Saudi Arabia", Flag: "🇸🇦", Confederation: "AFC", Group: "H"},
	{Name: "Spain", Flag: "🇪🇸", Confederation: "UEFA", Group: "H"},
	{Name: "Uruguay", Flag: "🇺🇾", Confederation: "CONMEBOL", Group: "H"},

	// Group I
	{Name: "France", Flag: "🇫🇷", Confederation: "UEFA", Group: "I"},
	{Name: "Iraq", Flag: "🇮🇶", Confederation: "AFC", Group: "I"},
	{Name: "Norway", Flag: "🇳🇴", Confederation: "UEFA", Group: "I"},
	{Name: "Senegal", Flag: "🇸🇳", Confederation: "CAF", Group: "I"},

	// Group J
	{Name: "Algeria", Flag: "🇩🇿", Confederation: "CAF", Group: "J"},
	{Name: "Argentina", Flag: "🇦🇷", Confederation: "CONMEBOL", Group: "J"},
	{Name: "Austria", Flag: "🇦🇹", Confederation: "UEFA", Group: "J"},
	{Name: "Jordan", Flag: "🇯🇴", Confederation: "AFC", Group: "J"},

	// Group K
	{Name: "Colombia", Flag: "🇨🇴", Confederation: "CONMEBOL", Group: "K"},
	{Name: "DR Congo", Flag: "🇨🇩", Confederation: "CAF", Group: "K"},
	{Name: "Portugal", Flag: "🇵🇹", Confederation: "UEFA", Group: "K"},
	{Name: "Uzbekistan", Flag: "🇺🇿", Confederation: "AFC", Group: "K"},

	// Group L
	{Name: "Croatia", Flag: "🇭🇷", Confederation: "UEFA", Group: "L"},
	{Name: "England", Flag: "🏴󠁧󠁢󠁥󠁮󠁧󠁿", Confederation: "UEFA", Group: "L"},
	{Name: "Ghana", Flag: "🇬🇭", Confederation: "CAF", Group: "L"},
	{Name: "Panama", Flag: "🇵🇦", Confederation: "CONCACAF", Group: "L"},
}

// teamIndex maps lowercase name → canonical name for O(1) lookups.
var teamIndex map[string]string

// teamAliases maps common variants / abbreviations to canonical names.
var teamAliases = map[string]string{
	"usa":                "United States",
	"us":                 "United States",
	"united states":      "United States",
	"korea republic":     "South Korea",
	"korea":              "South Korea",
	"cote d'ivoire":      "Ivory Coast",
	"côte d'ivoire":      "Ivory Coast",
	"türkiye":            "Turkey",
	"turkiye":            "Turkey",
	"czechia":            "Czech Republic",
	"bosnia-herzegovina": "Bosnia and Herzegovina",
	"holland":            "Netherlands",
	"cabo verde":         "Cape Verde",
	"congo dr":           "DR Congo",
	"dr congo":           "DR Congo",
	"congo":              "DR Congo",
	"bosnia":             "Bosnia and Herzegovina",
}

func init() {
	teamIndex = make(map[string]string, len(WC2026Teams))
	for _, t := range WC2026Teams {
		teamIndex[strings.ToLower(t.Name)] = t.Name
	}
}

// NormalizeTeamName returns the canonical team name for the given input,
// or empty string if the team is not in the WC 2026 qualified list.
func NormalizeTeamName(input string) string {
	key := strings.ToLower(strings.TrimSpace(input))
	if canonical, ok := teamIndex[key]; ok {
		return canonical
	}
	if canonical, ok := teamAliases[key]; ok {
		return canonical
	}
	return ""
}
