package data

import (
	"context"
	"fmt"
	"github.com/ncruces/go-sqlite3"
	"strings"
)

// PlayerCandidate describes a distinct same-name player using source statistics.
// ID is private query context; Description is the approved public distinction.
type PlayerSeasonDescription struct{ Year, Age, PlateAppearances int }
type PlayerCandidate struct {
	ID                int64
	Name, Description string
	Seasons           []PlayerSeasonDescription
}

func (d *Database) AmbiguousPlayers(ctx context.Context, question string) ([]PlayerCandidate, error) {
	c, err := sqlite3.OpenFlags(d.path, sqlite3.OPEN_READONLY)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(ctx, d.Timeout)
	defer cancel()
	c.SetInterrupt(ctx)
	stmt, _, err := c.Prepare(`SELECT p.player_id,p.name,s.year,s.player_age,s.pa FROM players p JOIN stats s ON s.player_id=p.player_id WHERE p.name IN (SELECT name FROM players GROUP BY name HAVING count(*)>1) ORDER BY p.player_id,s.year DESC`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	var candidates []PlayerCandidate
	seen := map[int64]int{}
	for stmt.Step() {
		id, name := stmt.ColumnInt64(0), stmt.ColumnText(1)
		parts := strings.SplitN(name, ", ", 2)
		if len(parts) == 2 {
			name = parts[1] + " " + parts[0]
		}
		if !strings.Contains(strings.ToLower(question), strings.ToLower(name)) {
			continue
		}
		season := PlayerSeasonDescription{Year: stmt.ColumnInt(2), Age: stmt.ColumnInt(3), PlateAppearances: stmt.ColumnInt(4)}
		if index, ok := seen[id]; ok {
			candidates[index].Seasons = append(candidates[index].Seasons, season)
			continue
		}
		seen[id] = len(candidates)
		candidates = append(candidates, PlayerCandidate{ID: id, Name: name, Seasons: []PlayerSeasonDescription{season}, Description: fmt.Sprintf("%s — age %d in %d, %d plate appearances", name, stmt.ColumnInt(3), stmt.ColumnInt(2), stmt.ColumnInt(4))})
	}
	return candidates, stmt.Err()
}
