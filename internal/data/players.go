package data

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// PlayerCandidate describes a distinct same-name player using source statistics.
// ID is private query context; Description is the approved public distinction.
type PlayerSeasonDescription struct{ Year, Age, PlateAppearances int }
type PlayerCandidate struct {
	ID                int64
	Name, Description string
	Seasons           []PlayerSeasonDescription
}

type catalogPlayer struct {
	key       string
	candidate PlayerCandidate
}

func normalizePlayerText(text string) string {
	decomposed := norm.NFD.String(text)
	plain := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, decomposed)
	return strings.Join(strings.Fields(cases.Fold().String(plain)), " ")
}

func containsPlayerName(question, key string) bool {
	word := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' }
	for offset := 0; offset < len(question); {
		i := strings.Index(question[offset:], key)
		if i < 0 {
			return false
		}
		start := offset + i
		end := start + len(key)
		before, _ := utf8.DecodeLastRuneInString(question[:start])
		after, _ := utf8.DecodeRuneInString(question[end:])
		if !word(before) && !word(after) {
			return true
		}
		offset = end
	}
	return false
}

// MatchedPlayers returns all database identities whose complete normalized name
// appears in the question. Colliding keys retain every distinct player ID.
func (d *Database) MatchedPlayers(question string) []PlayerCandidate {
	question = normalizePlayerText(question)
	var players []PlayerCandidate
	for _, player := range d.players {
		if containsPlayerName(question, player.key) {
			candidate := player.candidate
			candidate.Seasons = append([]PlayerSeasonDescription(nil), candidate.Seasons...)
			players = append(players, candidate)
		}
	}
	return players
}

func (d *Database) AmbiguousPlayers(ctx context.Context, question string) ([]PlayerCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	matched := d.MatchedPlayers(question)
	counts := map[string]int{}
	for _, player := range matched {
		counts[normalizePlayerText(player.Name)]++
	}
	var ambiguous []PlayerCandidate
	for _, player := range matched {
		if counts[normalizePlayerText(player.Name)] > 1 {
			ambiguous = append(ambiguous, player)
		}
	}
	return ambiguous, nil
}

var identityTokens = regexp.MustCompile(`\b[0-9]+\b`)

// ContainsPlayerIdentity checks actual private source identities, allowing
// unrelated public counts and source-precision numbers to remain readable.
func (d *Database) ContainsPlayerIdentity(text string) bool {
	for _, token := range identityTokens.FindAllString(text, -1) {
		if _, ok := d.playerIDs[token]; ok {
			return true
		}
	}
	return false
}

// MentionsPlayer distinguishes a newly named player from a revision that still
// needs the pending same-name candidates (for example, "the player age 23").
func (d *Database) MentionsPlayer(ctx context.Context, question string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return len(d.MatchedPlayers(question)) > 0, nil
}

// MissingPlayerSeason is safe answer context for an actually absent record.
// It deliberately carries no private identity.
type MissingPlayerSeason struct {
	Player string `json:"player"`
	Year   int    `json:"year"`
}

// VerifyPlayerCoverage distinguishes omitted available records from seasons
// absent in the catalog. Empty IDs mean a ranking or intentionally filtered set.
func VerifyPlayerCoverage(candidates []PlayerCandidate, ids []int64, seasons []int, result Result) ([]MissingPlayerSeason, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	byID := map[int64]PlayerCandidate{}
	requestedNames := map[string]bool{}
	for _, candidate := range candidates {
		byID[candidate.ID] = candidate
		requestedNames[normalizePlayerText(candidate.Name)] = false
	}
	selected := make([]PlayerCandidate, 0, len(ids))
	for _, id := range ids {
		candidate, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("player-season coverage: lookup identity is not in the matched catalog")
		}
		key := normalizePlayerText(candidate.Name)
		if requestedNames[key] {
			return nil, fmt.Errorf("player-season coverage: repeated or ambiguous lookup identity")
		}
		requestedNames[key] = true
		selected = append(selected, candidate)
	}
	for _, found := range requestedNames {
		if !found {
			return nil, fmt.Errorf("player-season coverage: lookup omitted a requested player")
		}
	}
	nameColumn, yearColumn, paColumn := -1, -1, -1
	for i, origin := range result.Origins {
		switch origin {
		case "name":
			nameColumn = i
		case "year":
			yearColumn = i
		case "pa":
			paColumn = i
		}
	}
	if nameColumn < 0 || yearColumn < 0 || paColumn < 0 || result.Truncated {
		return nil, fmt.Errorf("player-season coverage: select stored name, year and pa for a complete lookup")
	}
	type playerSeason struct {
		name string
		year int
	}
	returned := map[playerSeason]bool{}
	for _, row := range result.Rows {
		name, nameOK := row[nameColumn].(string)
		year, yearOK := row[yearColumn].(int64)
		if !nameOK || !yearOK {
			return nil, fmt.Errorf("player-season coverage: invalid name or season")
		}
		returned[playerSeason{name, int(year)}] = true
	}
	var missing []MissingPlayerSeason
	for _, candidate := range selected {
		for _, year := range seasons {
			available := false
			for _, season := range candidate.Seasons {
				if season.Year == year {
					available = true
					break
				}
			}
			if available {
				if !returned[playerSeason{candidate.Name, year}] {
					return nil, fmt.Errorf("player-season coverage: query omitted available statistics for %s in %d; filter by the supplied player IDs", candidate.Name, year)
				}
			} else {
				name := candidate.Name
				if parts := strings.SplitN(name, ", ", 2); len(parts) == 2 {
					name = parts[1] + " " + parts[0]
				}
				missing = append(missing, MissingPlayerSeason{Player: name, Year: year})
			}
		}
	}
	return missing, nil
}
