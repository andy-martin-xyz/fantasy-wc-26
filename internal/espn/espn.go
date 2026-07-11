// Package espn fetches World Cup match data from ESPN's free (unofficial)
// soccer API and reduces it to per-player fantasy stats. No API key required.
//
// Endpoint: site.api.espn.com/apis/site/v2/sports/soccer/fifa.world/summary?event={id}
package espn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	baseURL    = "https://site.api.espn.com/apis/site/v2/sports/soccer/fifa.world"
	summaryURL = baseURL + "/summary?event="
)

// TeamInfo identifies one national team in ESPN's World Cup data.
type TeamInfo struct {
	ID   string
	Name string
}

// RosterAthlete is one player on a national-team roster.
type RosterAthlete struct {
	ID       string
	Name     string
	Position string // GK/DEF/MID/FWD
}

// FetchTeams lists the World Cup national teams with their ESPN ids.
func FetchTeams(ctx context.Context) ([]TeamInfo, error) {
	var body struct {
		Sports []struct {
			Leagues []struct {
				Teams []struct {
					Team struct {
						ID          string `json:"id"`
						DisplayName string `json:"displayName"`
					} `json:"team"`
				} `json:"teams"`
			} `json:"leagues"`
		} `json:"sports"`
	}
	if err := getJSON(ctx, baseURL+"/teams", &body); err != nil {
		return nil, err
	}
	var out []TeamInfo
	for _, s := range body.Sports {
		for _, l := range s.Leagues {
			for _, t := range l.Teams {
				out = append(out, TeamInfo{ID: t.Team.ID, Name: t.Team.DisplayName})
			}
		}
	}
	return out, nil
}

// Fixture is one scheduled match from the scoreboard.
type Fixture struct {
	EventID  string
	Date     time.Time
	HomeTeam string // ESPN display name
	AwayTeam string // ESPN display name
	State    string // pre / in / post
}

// FetchFixtures returns World Cup fixtures for an ESPN `dates` value, which may
// be a single day (YYYYMMDD) or a range (YYYYMMDD-YYYYMMDD).
func FetchFixtures(ctx context.Context, dates string) ([]Fixture, error) {
	var body struct {
		Events []struct {
			ID           string `json:"id"`
			Date         string `json:"date"`
			Competitions []struct {
				Status struct {
					Type struct {
						State string `json:"state"`
					} `json:"type"`
				} `json:"status"`
				Competitors []struct {
					HomeAway string `json:"homeAway"`
					Team     struct {
						DisplayName string `json:"displayName"`
					} `json:"team"`
				} `json:"competitors"`
			} `json:"competitions"`
		} `json:"events"`
	}
	if err := getJSON(ctx, baseURL+"/scoreboard?limit=400&dates="+dates, &body); err != nil {
		return nil, err
	}
	var out []Fixture
	for _, e := range body.Events {
		if len(e.Competitions) == 0 {
			continue
		}
		c := e.Competitions[0]
		f := Fixture{EventID: e.ID, State: c.Status.Type.State, Date: parseESPNTime(e.Date)}
		for _, comp := range c.Competitors {
			if comp.HomeAway == "home" {
				f.HomeTeam = comp.Team.DisplayName
			} else {
				f.AwayTeam = comp.Team.DisplayName
			}
		}
		out = append(out, f)
	}
	return out, nil
}

// FetchRoster returns a national team's current squad (athlete id + name + position).
func FetchRoster(ctx context.Context, teamID string) ([]RosterAthlete, error) {
	var body struct {
		Athletes []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			Position    struct {
				Abbreviation string `json:"abbreviation"`
			} `json:"position"`
		} `json:"athletes"`
	}
	if err := getJSON(ctx, baseURL+"/teams/"+teamID+"/roster", &body); err != nil {
		return nil, err
	}
	out := make([]RosterAthlete, 0, len(body.Athletes))
	for _, a := range body.Athletes {
		out = append(out, RosterAthlete{ID: a.ID, Name: a.DisplayName, Position: mapPosition(a.Position.Abbreviation)})
	}
	return out, nil
}

// getJSON does a context-aware GET and decodes the body.
func getJSON(ctx context.Context, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("espn returned status %d for %s", resp.StatusCode, url)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// AthleteStat is one player's events in a match.
type AthleteStat struct {
	ESPNID   string
	Name     string
	Position string // GK/DEF/MID/FWD (mapped from ESPN G/D/M/F), "" if unknown
	Played   bool
	Goals    int
	Assists  int
	Yellow   int
	Red      int
}

// TeamSide is one team's line in a match.
type TeamSide struct {
	ID       string
	Name     string
	Score    int
	Athletes []*AthleteStat
}

// Summary is the reduced match result.
type Summary struct {
	State     string // "pre" | "in" | "post" — from ESPN's status.type.state
	Completed bool   // true when State == "post"
	Home      TeamSide
	Away      TeamSide
}

// raw mirrors the subset of ESPN's summary JSON we consume.
type raw struct {
	Header struct {
		Competitions []struct {
			Status struct {
				Type struct {
					State     string `json:"state"`
					Completed bool   `json:"completed"`
				} `json:"type"`
			} `json:"status"`
			Competitors []struct {
				HomeAway string `json:"homeAway"`
				Score    string `json:"score"`
				Team     struct {
					ID          string `json:"id"`
					DisplayName string `json:"displayName"`
				} `json:"team"`
			} `json:"competitors"`
		} `json:"competitions"`
	} `json:"header"`
	Rosters []struct {
		HomeAway string `json:"homeAway"`
		Team     struct {
			ID string `json:"id"`
		} `json:"team"`
		Roster []struct {
			Starter  bool `json:"starter"`
			SubbedIn bool `json:"subbedIn"`
			Athlete  struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
			} `json:"athlete"`
			Position struct {
				Abbreviation string `json:"abbreviation"`
			} `json:"position"`
		} `json:"roster"`
	} `json:"rosters"`
	KeyEvents []struct {
		Type struct {
			Text string `json:"text"`
		} `json:"type"`
		Shootout bool `json:"shootout"`
		Team     struct {
			ID string `json:"id"`
		} `json:"team"`
		Participants []struct {
			Athlete struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
			} `json:"athlete"`
		} `json:"participants"`
	} `json:"keyEvents"`
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

// FetchSummary retrieves and reduces a match summary by ESPN event id.
func FetchSummary(ctx context.Context, eventID string) (*Summary, error) {
	if strings.TrimSpace(eventID) == "" {
		return nil, fmt.Errorf("empty event id")
	}
	var r raw
	if err := getJSON(ctx, summaryURL+eventID, &r); err != nil {
		return nil, err
	}
	if len(r.Header.Competitions) == 0 {
		return nil, fmt.Errorf("no competition in espn summary (bad event id?)")
	}

	comp := r.Header.Competitions[0]
	s := &Summary{
		State:     comp.Status.Type.State,
		Completed: comp.Status.Type.Completed || comp.Status.Type.State == "post",
	}

	// Home/away identity + score.
	for _, c := range comp.Competitors {
		side := &s.Away
		if c.HomeAway == "home" {
			side = &s.Home
		}
		side.ID = c.Team.ID
		side.Name = c.Team.DisplayName
		side.Score, _ = strconv.Atoi(c.Score)
	}

	// Index athletes per team id so events attach to a single struct.
	idx := map[string]*AthleteStat{} // "teamID|athleteID" -> stat
	teamOf := func(teamID string) *TeamSide {
		switch teamID {
		case s.Home.ID:
			return &s.Home
		case s.Away.ID:
			return &s.Away
		}
		return nil
	}
	get := func(teamID, athleteID, name string) *AthleteStat {
		key := teamID + "|" + athleteID
		if a, ok := idx[key]; ok {
			return a
		}
		a := &AthleteStat{ESPNID: athleteID, Name: name}
		idx[key] = a
		if t := teamOf(teamID); t != nil {
			t.Athletes = append(t.Athletes, a)
		}
		return a
	}

	// Rosters → who appeared + position.
	for _, rt := range r.Rosters {
		for _, e := range rt.Roster {
			if !e.Starter && !e.SubbedIn {
				continue // unused bench player
			}
			a := get(rt.Team.ID, e.Athlete.ID, e.Athlete.DisplayName)
			a.Played = true
			a.Position = mapPosition(e.Position.Abbreviation)
		}
	}

	// Key events → goals, assists, cards. Skip shootout (penalty-kick decider).
	for _, e := range r.KeyEvents {
		if e.Shootout || len(e.Participants) == 0 {
			continue
		}
		t := e.Type.Text
		primary := e.Participants[0].Athlete
		switch {
		case strings.Contains(t, "Own Goal"):
			// No fantasy credit to the scorer; the conceded goal is already in the score.
			continue
		case strings.Contains(t, "Goal"), strings.Contains(t, "Penalty - Scored"):
			a := get(e.Team.ID, primary.ID, primary.DisplayName)
			a.Played, a.Goals = true, a.Goals+1
			if len(e.Participants) >= 2 { // assist
				as := e.Participants[1].Athlete
				b := get(e.Team.ID, as.ID, as.DisplayName)
				b.Played, b.Assists = true, b.Assists+1
			}
		case strings.Contains(t, "Red"): // covers "Red Card" and "Yellow Red Card"
			a := get(e.Team.ID, primary.ID, primary.DisplayName)
			a.Played, a.Red = true, a.Red+1
		case strings.Contains(t, "Yellow"):
			a := get(e.Team.ID, primary.ID, primary.DisplayName)
			a.Played, a.Yellow = true, a.Yellow+1
		}
	}

	return s, nil
}

// parseESPNTime handles ESPN's timestamps, which omit seconds
// (e.g. "2026-06-11T19:00Z") and so don't match time.RFC3339.
func parseESPNTime(s string) time.Time {
	for _, layout := range []string{"2006-01-02T15:04Z07:00", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func mapPosition(abbr string) string {
	switch strings.ToUpper(strings.TrimSpace(abbr)) {
	case "G", "GK":
		return "GK"
	case "D", "DEF", "CB", "LB", "RB", "LWB", "RWB":
		return "DEF"
	case "M", "MID", "CM", "CDM", "CAM", "LM", "RM":
		return "MID"
	case "F", "FW", "ST", "CF", "LW", "RW":
		return "FWD"
	}
	return ""
}
