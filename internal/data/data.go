// Package data imports and reads fixed season snapshots. Runtime connections are
// read-only and authorize only baseball data and a small set of SQL functions.
package data

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3"
)

type Metric struct {
	Name, Label string
	Integer     bool
	Decimals    int
}

var Metrics = []Metric{
	{"player_age", "Age", true, 0}, {"ab", "At bats", true, 0}, {"pa", "Plate appearances", true, 0}, {"hit", "Hits", true, 0}, {"single", "Singles", true, 0}, {"double", "Doubles", true, 0}, {"triple", "Triples", true, 0}, {"home_run", "Home runs", true, 0}, {"strikeout", "Strikeouts", true, 0}, {"walk", "Walks", true, 0},
	{"k_percent", "Strikeout percentage", false, 1}, {"bb_percent", "Walk percentage", false, 1}, {"batting_avg", "Batting average", false, 3}, {"slg_percent", "Slugging percentage", false, 3}, {"on_base_percent", "On-base percentage", false, 3}, {"on_base_plus_slg", "OPS", false, 3}, {"babip", "Batting average on balls in play", false, 3}, {"b_rbi", "Runs batted in", true, 0}, {"r_total_stolen_base", "Stolen bases", true, 0},
	{"xba", "Expected batting average", false, 3}, {"xslg", "Expected slugging percentage", false, 3}, {"woba", "Weighted on-base average", false, 3}, {"xwoba", "Expected weighted on-base average", false, 3}, {"xobp", "Expected on-base percentage", false, 3}, {"xiso", "Expected isolated power", false, 3},
	{"avg_swing_speed", "Average swing speed", false, -1}, {"fast_swing_rate", "Fast swing rate", false, -1}, {"blasts_contact", "Blasts per contact", false, -1}, {"blasts_swing", "Blasts per swing", false, -1}, {"squared_up_contact", "Squared-up contact", false, -1}, {"squared_up_swing", "Squared-up swings", false, -1}, {"avg_swing_length", "Average swing length", false, -1}, {"swords", "Swords", true, 0}, {"attack_angle", "Attack angle", false, -1}, {"attack_direction", "Attack direction", false, -1}, {"ideal_angle_rate", "Ideal angle rate", false, -1}, {"vertical_swing_path", "Vertical swing path", false, -1}, {"exit_velocity_avg", "Average exit velocity", false, -1}, {"launch_angle_avg", "Average launch angle", false, -1}, {"sweet_spot_percent", "Sweet spot percentage", false, -1}, {"barrel_batted_rate", "Barrel rate", false, -1}, {"hard_hit_percent", "Hard-hit percentage", false, -1}, {"avg_best_speed", "Average best speed", false, -1}, {"avg_hyper_speed", "Average hyper speed", false, -1}, {"whiff_percent", "Whiff percentage", false, -1}, {"swing_percent", "Swing percentage", false, -1},
}

func Schema() string {
	parts := []string{"player_id INTEGER NOT NULL REFERENCES players(player_id)", "year INTEGER NOT NULL"}
	for _, m := range Metrics {
		typ := "REAL"
		if m.Integer {
			typ = "INTEGER"
		}
		parts = append(parts, m.Name+" "+typ+" NOT NULL")
	}
	parts = append(parts, "PRIMARY KEY (player_id, year)")
	return "CREATE TABLE players (player_id INTEGER PRIMARY KEY, name TEXT NOT NULL);\nCREATE TABLE stats (" + strings.Join(parts, ", ") + ");"
}

// Import validates the complete export before atomically replacing the database.
func Import(source, path string) (err error) {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".mlb-import-*.db")
	if err != nil {
		return err
	}
	name := temp.Name()
	temp.Close()
	defer os.Remove(name)
	c, err := sqlite3.Open(name)
	if err != nil {
		return err
	}
	defer c.Close()
	if err = c.Exec("PRAGMA foreign_keys=ON; BEGIN; " + Schema()); err != nil {
		return err
	}
	content, err := io.ReadAll(input)
	if err != nil {
		return err
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(content), "\ufeff")))
	head, err := reader.Read()
	if err != nil {
		return err
	}
	head[0] = strings.TrimPrefix(head[0], "\ufeff")
	want := []string{"last_name, first_name", "player_id", "year"}
	for _, m := range Metrics {
		want = append(want, m.Name)
	}
	// Compatible exports may omit singles. Restore that source column from
	// the other hit counts, while keeping the same complete stored schema.
	deriveSingles := len(head) == len(want)-1
	singleColumn := -1
	if deriveSingles {
		for i, name := range want {
			if name == "single" {
				singleColumn = i
				break
			}
		}
		head = append(head[:singleColumn:singleColumn], append([]string{"single"}, head[singleColumn:]...)...)
	}
	if len(head) != len(want) {
		return errors.New("export has unexpected columns")
	}
	for i := range want {
		if head[i] != want[i] {
			return fmt.Errorf("unexpected column %d: %s", i, head[i])
		}
	}
	player, _, err := c.Prepare("INSERT INTO players(player_id,name) VALUES (?,?) ON CONFLICT(player_id) DO NOTHING")
	if err != nil {
		return err
	}
	defer player.Close()
	checks, _, err := c.Prepare("SELECT name FROM players WHERE player_id=?")
	if err != nil {
		return err
	}
	defer checks.Close()
	fields := []string{"player_id", "year"}
	marks := []string{"?", "?"}
	for _, m := range Metrics {
		fields = append(fields, m.Name)
		marks = append(marks, "?")
	}
	stats, _, err := c.Prepare("INSERT INTO stats(" + strings.Join(fields, ",") + ") VALUES (" + strings.Join(marks, ",") + ")")
	if err != nil {
		return err
	}
	defer stats.Close()
	count := 0
	for {
		row, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		count++
		if deriveSingles {
			row = append(row[:singleColumn:singleColumn], append([]string{""}, row[singleColumn:]...)...)
			var singles int64
			for _, name := range []string{"hit", "double", "triple", "home_run"} {
				index := 0
				for want[index] != name {
					index++
				}
				value, e := strconv.ParseInt(row[index], 10, 64)
				if e != nil || value < 0 {
					return fmt.Errorf("invalid %s at row %d", name, count)
				}
				if name == "hit" {
					singles = value
				} else {
					singles -= value
				}
				if singles < 0 {
					return fmt.Errorf("inconsistent hit counts at row %d", count)
				}
			}
			row[singleColumn] = strconv.FormatInt(singles, 10)
		}
		if strings.TrimSpace(row[0]) == "" {
			return fmt.Errorf("row %d has no player name", count)
		}
		id, e := strconv.ParseInt(row[1], 10, 64)
		if e != nil || id <= 0 {
			return fmt.Errorf("row %d has invalid player ID", count)
		}
		year, e := strconv.Atoi(row[2])
		if e != nil || year < 1900 || year > 3000 {
			return fmt.Errorf("row %d has invalid season", count)
		}
		player.BindInt64(1, id)
		player.BindText(2, row[0])
		if e = player.Exec(); e != nil {
			return e
		}
		player.Reset()
		checks.BindInt64(1, id)
		if !checks.Step() || checks.ColumnText(0) != row[0] {
			return fmt.Errorf("inconsistent player name for %d", id)
		}
		checks.Reset()
		stats.BindInt64(1, id)
		stats.BindInt(2, year)
		for i, m := range Metrics {
			raw := row[i+3]
			if m.Integer {
				v, e := strconv.ParseInt(raw, 10, 64)
				if e != nil || v < 0 {
					return fmt.Errorf("invalid %s at row %d", m.Name, count)
				}
				stats.BindInt64(i+3, v)
			} else {
				v, e := strconv.ParseFloat(raw, 64)
				if e != nil || math.IsNaN(v) || math.IsInf(v, 0) {
					return fmt.Errorf("invalid %s at row %d", m.Name, count)
				}
				stats.BindFloat(i+3, v)
			}
		}
		if e = stats.Exec(); e != nil {
			return fmt.Errorf("invalid or duplicate player-season row %d: %w", count, e)
		}
		stats.Reset()
	}
	if count == 0 {
		return errors.New("export has no player-seasons")
	}
	if err = c.Exec("COMMIT"); err != nil {
		return err
	}
	player.Close()
	checks.Close()
	stats.Close()
	if err = c.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

type Database struct {
	path      string
	Seasons   []int
	Timeout   time.Duration
	playerIDs map[string]struct{}
}
type Result struct {
	Columns   []string
	Origins   []string
	Rows      [][]any
	Truncated bool
}

func Open(path string) (*Database, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	c, err := sqlite3.OpenFlags(abs, sqlite3.OPEN_READONLY)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	stmt, _, err := c.Prepare("SELECT DISTINCT year FROM stats ORDER BY year")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	seasons := []int{}
	for stmt.Step() {
		seasons = append(seasons, stmt.ColumnInt(0))
	}
	if err = stmt.Err(); err != nil {
		return nil, err
	}
	if len(seasons) == 0 {
		return nil, errors.New("empty database")
	}
	identities, _, err := c.Prepare("SELECT player_id FROM players")
	if err != nil {
		return nil, err
	}
	defer identities.Close()
	playerIDs := map[string]struct{}{}
	for identities.Step() {
		playerIDs[strconv.FormatInt(identities.ColumnInt64(0), 10)] = struct{}{}
	}
	if err := identities.Err(); err != nil {
		return nil, err
	}
	return &Database{path: abs, Seasons: seasons, Timeout: 10 * time.Second, playerIDs: playerIDs}, nil
}
func (d *Database) Close() error { return nil }
func (d *Database) Query(ctx context.Context, sql string) (Result, error) {
	result := Result{Rows: [][]any{}}
	if strings.ContainsRune(sql, 0) {
		return result, errors.New("invalid query")
	}
	ctx, cancel := context.WithTimeout(ctx, d.Timeout)
	defer cancel()
	c, err := sqlite3.OpenFlags(d.path, sqlite3.OPEN_READONLY)
	if err != nil {
		return result, err
	}
	defer c.Close()
	c.SetInterrupt(ctx)
	err = c.SetAuthorizer(func(a sqlite3.AuthorizerActionCode, x, y, db, inner string) sqlite3.AuthorizerReturnCode {
		switch a {
		case sqlite3.AUTH_SELECT, sqlite3.AUTH_RECURSIVE:
			return sqlite3.AUTH_OK
		case sqlite3.AUTH_READ:
			if db == "main" && (x == "players" || x == "stats") {
				return sqlite3.AUTH_OK
			}
		case sqlite3.AUTH_FUNCTION:
			if strings.Contains("|abs|round|coalesce|ifnull|nullif|max|min|sum|avg|count|dense_rank|rank|row_number|lag|lead|lower|upper|trim|length|like|instr|substr|replace|", "|"+strings.ToLower(y)+"|") {
				return sqlite3.AUTH_OK
			}
		}
		return sqlite3.AUTH_DENY
	})
	if err != nil {
		return result, err
	}
	stmt, tail, err := c.Prepare(sql)
	if err != nil {
		return result, err
	}
	if stmt == nil {
		return result, errors.New("empty query")
	}
	defer stmt.Close()
	if !stmt.ReadOnly() {
		return result, errors.New("query must be read-only")
	}
	// SQLite itself parses the tail; comments and trailing semicolons are safe,
	// while a second executable statement is rejected before any row is read.
	for strings.TrimSpace(tail) != "" {
		extra, next, e := c.Prepare(tail)
		if e != nil {
			return result, e
		}
		if extra != nil {
			extra.Close()
			return result, errors.New("only one query is permitted")
		}
		if next == tail {
			break
		}
		tail = next
	}
	for i := 0; i < stmt.ColumnCount(); i++ {
		result.Columns = append(result.Columns, stmt.ColumnName(i))
		origin := stmt.ColumnOriginName(i)
		if origin == "player_id" {
			return result, errors.New("identity columns are private")
		}
		result.Origins = append(result.Origins, origin)
	}
	for stmt.Step() {
		if len(result.Rows) == 500 {
			result.Truncated = true
			break
		}
		row := make([]any, stmt.ColumnCount())
		for i := range row {
			switch stmt.ColumnType(i) {
			case sqlite3.INTEGER:
				row[i] = stmt.ColumnInt64(i)
			case sqlite3.FLOAT:
				row[i] = stmt.ColumnFloat(i)
			case sqlite3.TEXT:
				row[i] = stmt.ColumnText(i)
			case sqlite3.NULL:
				row[i] = nil
			default:
				return result, errors.New("unsupported result value")
			}
		}
		result.Rows = append(result.Rows, row)
	}
	if err = stmt.Err(); err != nil {
		return Result{}, err
	}
	return result, nil
}
