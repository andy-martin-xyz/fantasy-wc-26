package models

import "time"

// --- User -------------------------------------------------------

type User struct {
	UID         string    `firestore:"uid"         json:"uid"`
	DisplayName string    `firestore:"displayName" json:"displayName"`
	Email       string    `firestore:"email"       json:"email"`
	PhotoURL    string    `firestore:"photoURL"    json:"photoURL"`
	TeamName    string    `firestore:"teamName"    json:"teamName"`
	CreatedAt   time.Time `firestore:"createdAt"   json:"createdAt"`
	IsAdmin     bool      `firestore:"isAdmin"     json:"isAdmin"`
}

// --- Player -----------------------------------------------------

type Player struct {
	ID          string `firestore:"id"          json:"id"`
	Name        string `firestore:"name"        json:"name"`
	Country     string `firestore:"country"     json:"country"`
	Position    string `firestore:"position"    json:"position"` // GK DEF MID FWD
	ClubTeam    string `firestore:"clubTeam"    json:"clubTeam"`
	Rating      int    `firestore:"rating"      json:"rating"`
	ImageURL    string `firestore:"imageURL"    json:"imageURL"`
	Drafted     bool   `firestore:"drafted"     json:"drafted"`
	DraftedBy   string `firestore:"draftedBy"   json:"draftedBy"`
	TotalPoints int    `firestore:"totalPoints" json:"totalPoints"`
}

// Valid positions.
var ValidPositions = map[string]bool{"GK": true, "DEF": true, "MID": true, "FWD": true}

// Roster limits per position.
var PositionLimits = map[string]int{"GK": 1, "DEF": 4, "MID": 4, "FWD": 2}

// --- Draft ------------------------------------------------------

type DraftConfig struct {
	Status               string    `firestore:"status"               json:"status"` // pending active paused complete
	CurrentPickIndex     int       `firestore:"currentPickIndex"     json:"currentPickIndex"`
	PickOrder            []string  `firestore:"pickOrder"            json:"pickOrder"` // uids in round-1 order
	PickTimeLimitSeconds int       `firestore:"pickTimeLimitSeconds" json:"pickTimeLimitSeconds"`
	CurrentPickDeadline  time.Time `firestore:"currentPickDeadline"  json:"currentPickDeadline"`
	Round                int       `firestore:"round"                json:"round"`
}

// CurrentPickerUID returns the UID whose turn it is, using snake draft order.
func (d *DraftConfig) CurrentPickerUID() string {
	n := len(d.PickOrder)
	if n == 0 {
		return ""
	}
	i := d.CurrentPickIndex
	round := i / n
	posInRound := i % n
	if round%2 == 0 {
		return d.PickOrder[posInRound]
	}
	return d.PickOrder[n-1-posInRound]
}

// ComputeRound returns the 1-indexed round for the current pick index.
func (d *DraftConfig) ComputeRound() int {
	n := len(d.PickOrder)
	if n == 0 {
		return 1
	}
	return d.CurrentPickIndex/n + 1
}

type DraftPick struct {
	UserID     string    `firestore:"userId"     json:"userId"`
	PlayerID   string    `firestore:"playerId"   json:"playerId"`
	PlayerName string    `firestore:"playerName" json:"playerName"`
	Round      int       `firestore:"round"      json:"round"`
	PickNumber int       `firestore:"pickNumber" json:"pickNumber"`
	Timestamp  time.Time `firestore:"timestamp"  json:"timestamp"`
}

// --- Roster -----------------------------------------------------

type RosterPlayer struct {
	PlayerID string `firestore:"playerId" json:"playerId"`
	Name     string `firestore:"name"     json:"name"`
	Country  string `firestore:"country"  json:"country"`
	Position string `firestore:"position" json:"position"`
}

type Roster struct {
	UserID      string         `firestore:"userId"      json:"userId"`
	TeamName    string         `firestore:"teamName"    json:"teamName"`
	Players     []RosterPlayer `firestore:"players"     json:"players"`
	TotalPoints int            `firestore:"totalPoints" json:"totalPoints"`
}

// --- Match & Scoring --------------------------------------------

type Match struct {
	ID               string    `firestore:"id"               json:"id"`
	HomeTeam         string    `firestore:"homeTeam"         json:"homeTeam"`
	AwayTeam         string    `firestore:"awayTeam"         json:"awayTeam"`
	HomeScore        int       `firestore:"homeScore"        json:"homeScore"`
	AwayScore        int       `firestore:"awayScore"        json:"awayScore"`
	Stage            string    `firestore:"stage"            json:"stage"` // group r16 quarter semi final
	Date             time.Time `firestore:"date"             json:"date"`
	Status           string    `firestore:"status"           json:"status"` // upcoming live complete
	ScoringProcessed bool      `firestore:"scoringProcessed" json:"scoringProcessed"`
}

type PlayerMatchStats struct {
	PlayerID      string `firestore:"playerId"      json:"playerId"`
	MatchID       string `firestore:"matchId"       json:"matchId"`
	Goals         int    `firestore:"goals"         json:"goals"`
	Assists       int    `firestore:"assists"       json:"assists"`
	CleanSheet    bool   `firestore:"cleanSheet"    json:"cleanSheet"`
	YellowCards   int    `firestore:"yellowCards"   json:"yellowCards"`
	RedCards      int    `firestore:"redCards"      json:"redCards"`
	TeamWin       bool   `firestore:"teamWin"       json:"teamWin"`
	TeamDraw      bool   `firestore:"teamDraw"      json:"teamDraw"`
	PointsAwarded int    `firestore:"pointsAwarded" json:"pointsAwarded"`
}

// CalculatePoints applies the scoring rules for a player's match stats.
func CalculatePoints(s PlayerMatchStats, position string) int {
	pts := 0
	pts += s.Goals * 5
	pts += s.Assists * 3
	if s.CleanSheet && (position == "GK" || position == "DEF") {
		pts += 4
	}
	pts -= s.YellowCards * 1
	pts -= s.RedCards * 3
	if s.TeamWin {
		pts += 2
	} else if s.TeamDraw {
		pts += 1
	}
	return pts
}

// --- Leaderboard ------------------------------------------------

type LeaderboardEntry struct {
	UserID      string `firestore:"userId"      json:"userId"`
	TeamName    string `firestore:"teamName"    json:"teamName"`
	DisplayName string `firestore:"displayName" json:"displayName"`
	PhotoURL    string `firestore:"photoURL"    json:"photoURL"`
	TotalPoints int    `firestore:"totalPoints" json:"totalPoints"`
}

type Leaderboard struct {
	Standings []LeaderboardEntry `firestore:"standings" json:"standings"`
}
