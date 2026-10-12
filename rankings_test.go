package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// Controlled provider responses exercise the public HTTP workflow and real SDK
// without an API key or a paid request. Expected values are held-out literals.
func rankingApp(t *testing.T, plan QueryPlan, answer string, source ...string) httpHandler {
	t.Helper()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Text struct {
				Format struct {
					Name string `json:"name"`
				} `json:"format"`
			} `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		var result any = plan
		if payload.Text.Format.Name == "answer" {
			result = AnswerPresentation{Text: answer}
		}
		raw, err := json.Marshal(result)
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "resp_test", "object": "response", "status": "completed", "output": []any{map[string]any{"type": "message", "id": "msg_test", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": string(raw), "annotations": []any{}}}}}})
	}))
	t.Cleanup(provider.Close)
	model := &OpenAIModel{Client: openai.NewClient(option.WithAPIKey("test-only"), option.WithBaseURL(provider.URL+"/"), option.WithMaxRetries(0)), ModelName: "test-model"}
	if len(source) == 0 {
		_, app := testApp(t, model)
		return app
	}
	path := filepath.Join(t.TempDir(), "mlb.db")
	if err := ImportCSV(source[0], path); err != nil {
		t.Fatal(err)
	}
	db, err := OpenDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewHandler(NewQueryService(db, model, Config{Model: "test-model", DefaultStrategy: StrategySchema}, log.New(io.Discard, "", 0)))
}

func askRanking(t *testing.T, app httpHandler, question string) Outcome {
	t.Helper()
	raw, _ := json.Marshal(QuestionRequest{Question: question})
	resp := httptest.NewRecorder()
	app.ServeHTTP(resp, httptest.NewRequest("POST", "/api/questions", strings.NewReader(string(raw))))
	var outcome Outcome
	if err := json.Unmarshal(resp.Body.Bytes(), &outcome); err != nil {
		t.Fatal(err)
	}
	if resp.Code != http.StatusOK || outcome.Kind != "answer" || len(outcome.Strategies) != 1 || outcome.Strategies[0].Kind != "answer" {
		t.Fatalf("unexpected response: %s", resp.Body.String())
	}
	return outcome
}

func TestBrowserIncludesBothPlayersTiedAtDenseRankTen(t *testing.T) {
	app := rankingApp(t, QueryPlan{Status: "query", SQL: `WITH ranked AS (
 SELECT p.name, s.year, s.home_run, s.pa, DENSE_RANK() OVER (ORDER BY s.home_run DESC) AS arbitrary_private_alias
 FROM players p JOIN stats s ON p.player_id=s.player_id WHERE s.year=2024 AND s.pa>=502
 ) SELECT name, year, home_run, pa, arbitrary_private_alias FROM ranked WHERE arbitrary_private_alias=10 ORDER BY name`, Seasons: []int{2024}, Columns: []string{"player", "season", "home_run", "pa", "rank"}}, "Pete Alonso and Cal Raleigh share tenth place with 34 home runs in the 2024 snapshot.")
	outcome := askRanking(t, app, "Which qualifying players share tenth place by dense rank for home runs in 2024?")
	table := outcome.Strategies[0].Table
	want := [][]string{{"Pete Alonso", "2024", "34", "695", "10"}, {"Cal Raleigh", "2024", "34", "628", "10"}}
	if table == nil || !reflect.DeepEqual(table.Rows, want) || !reflect.DeepEqual(table.Columns, []string{"Player", "Season", "Home runs", "Plate appearances", "Rank"}) {
		t.Fatalf("tied rank table: %+v", table)
	}
}

func TestBrowserCountsQualifyingPlayersAcrossSuppliedSeasons(t *testing.T) {
	app := rankingApp(t, QueryPlan{Status: "query", SQL: "SELECT year, COUNT(*) AS untrusted_count_alias FROM stats WHERE pa>=502 GROUP BY year ORDER BY year", Seasons: []int{2024, 2025, 2026}, Columns: []string{"season", "player_count"}}, "The imported snapshots have 129 qualifying players in 2024, 145 in 2025, and 135 in 2026.")
	outcome := askRanking(t, app, "How many players had at least 502 plate appearances in each available season?")
	table := outcome.Strategies[0].Table
	if table == nil || !reflect.DeepEqual(table.Columns, []string{"Season", "Players"}) || !reflect.DeepEqual(table.Rows, [][]string{{"2024", "129"}, {"2025", "145"}, {"2026", "135"}}) {
		t.Fatalf("qualifying counts: %+v", table)
	}
}

func TestBrowserAnswersMLBHomeRunLeadersAcrossSeasons(t *testing.T) {
	app := rankingApp(t, QueryPlan{Status: "query", SQL: `WITH ranked AS (
 SELECT p.name,s.year,s.home_run,s.pa,DENSE_RANK() OVER (PARTITION BY s.year ORDER BY s.home_run DESC) AS placement
 FROM stats s JOIN players p ON p.player_id=s.player_id WHERE s.pa>=502
 ) SELECT name,year,home_run,pa FROM ranked WHERE placement=1 ORDER BY year,name`, Seasons: []int{2024, 2025, 2026}}, "The snapshot leaders were Aaron Judge in 2024, Cal Raleigh in 2025, and Kyle Schwarber and Pete Crow-Armstrong tied in 2026.")
	outcome := askRanking(t, app, "Who led the league in home runs in 2024, 2025, and 2026?")
	want := [][]string{{"Aaron Judge", "2024", "58", "704"}, {"Cal Raleigh", "2025", "60", "705"}, {"Pete Crow-Armstrong", "2026", "45", "726"}, {"Kyle Schwarber", "2026", "45", "701"}}
	if !reflect.DeepEqual(outcome.Seasons, []int{2024, 2025, 2026}) || !reflect.DeepEqual(outcome.Strategies[0].Table.Rows, want) {
		t.Fatalf("multi-season leaders: %+v", outcome)
	}
}

func TestBrowserPreservesExportedOPSForLatestSeasonLeader(t *testing.T) {
	app := rankingApp(t, QueryPlan{Status: "query", SQL: `WITH ranked AS (
 SELECT p.name,s.year,s.on_base_plus_slg,s.pa,DENSE_RANK() OVER (ORDER BY s.on_base_plus_slg DESC) AS placement
 FROM stats s JOIN players p ON p.player_id=s.player_id WHERE s.year=(SELECT MAX(year) FROM stats) AND s.pa>=502
 ) SELECT name,year,on_base_plus_slg,pa FROM ranked WHERE placement=1`, Seasons: []int{2026}}, "Using the latest available season, 2026, Yordan Alvarez led MLB with an exported OPS of 1.035 over 691 plate appearances in the snapshot.")
	outcome := askRanking(t, app, "Who led MLB in OPS?")
	if !reflect.DeepEqual(outcome.Seasons, []int{2026}) || !reflect.DeepEqual(outcome.Strategies[0].Table.Rows, [][]string{{"Yordan Alvarez", "2026", "1.035", "691"}}) || !strings.Contains(outcome.Strategies[0].Message, "2026") {
		t.Fatalf("default season OPS: %+v", outcome)
	}
}

func TestBrowserTreatsTopTenHRSinglesDoublesOverlapAsNormalEmptyAnswer(t *testing.T) {
	for _, season := range []int{2024, 2025, 2026} {
		t.Run(strconv.Itoa(season), func(t *testing.T) {
			sql := fmt.Sprintf(`WITH ranked AS (
    SELECT p.name,s.year,s.home_run,s.single,s.double,s.pa,
     DENSE_RANK() OVER (ORDER BY s.home_run DESC) AS hr_rank,
     DENSE_RANK() OVER (ORDER BY s.single DESC) AS singles_rank,
     DENSE_RANK() OVER (ORDER BY s.double DESC) AS doubles_rank
    FROM stats s JOIN players p ON p.player_id=s.player_id WHERE s.pa>=502 AND s.year=%d
   ) SELECT name,year,home_run,single,double,pa FROM ranked WHERE hr_rank<=10 AND singles_rank<=10 AND doubles_rank<=10 ORDER BY name`, season)
			app := rankingApp(t, QueryPlan{Status: "query", SQL: sql, Seasons: []int{season}}, "No qualifying players appear in all three top-ten lists in this season snapshot.")
			outcome := askRanking(t, app, fmt.Sprintf("Is any qualifying player in the top ten for HR, 1B, and 2B in %d?", season))
			table := outcome.Strategies[0].Table
			if table == nil || len(table.Rows) != 0 || !reflect.DeepEqual(table.Columns, []string{"Player", "Season", "Home runs", "Singles", "Doubles", "Plate appearances"}) {
				t.Fatalf("empty overlap: %+v", outcome)
			}
		})
	}
}

func TestBrowserAnswersAdvancedMetricWithSourcePrecision(t *testing.T) {
	app := rankingApp(t, QueryPlan{Status: "query", SQL: "SELECT p.name,s.year,s.avg_best_speed,s.pa FROM stats s JOIN players p ON p.player_id=s.player_id WHERE p.player_id=664761 AND s.year=2024", Seasons: []int{2024}}, "The 2024 imported snapshot records average best speed of 100.768220847.")
	outcome := askRanking(t, app, "What was Alec Bohm's average best speed in 2024?")
	table := outcome.Strategies[0].Table
	if table == nil || table.Columns[2] != "Average best speed" || table.Rows[0][2] != "100.768220847" {
		t.Fatalf("advanced metric: %+v", table)
	}
}

func TestBrowserTreatsNoPlayerAboveHomeRunThresholdAsNormalAnswer(t *testing.T) {
	app := rankingApp(t, QueryPlan{Status: "query", SQL: "SELECT p.name,s.year,s.home_run,s.pa FROM stats s JOIN players p ON p.player_id=s.player_id WHERE s.year=2026 AND s.pa>=502 AND s.home_run>100", Seasons: []int{2026}}, "No qualifying players hit more than 100 home runs in the 2026 snapshot.")
	outcome := askRanking(t, app, "Which qualifying players hit more than 100 home runs in 2026?")
	if len(outcome.Strategies[0].Table.Rows) != 0 {
		t.Fatalf("nonempty threshold result: %+v", outcome)
	}
}

func TestBrowserExcludesHighStatNonqualifierBeforeRankingEqualExportedRates(t *testing.T) {
	// This controlled fixture gives two qualifiers equal exported OPS while their
	// component statistics differ; a third player has higher OPS with only 501 PA.
	raw, err := os.ReadFile("advanced-mlb-24-26.csv")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), "\ufeff"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]int{}
	for i, name := range rows[0] {
		fields[name] = i
	}
	for _, row := range rows[1:] {
		if row[fields["year"]] != "2026" {
			continue
		}
		switch row[fields["player_id"]] {
		case "670541", "592450":
			row[fields["on_base_plus_slg"]] = "1.111"
			row[fields["pa"]] = "600"
		case "656941":
			row[fields["on_base_plus_slg"]] = "2.000"
			row[fields["pa"]] = "501"
		}
	}
	path := filepath.Join(t.TempDir(), "rate-ties.csv")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := csv.NewWriter(f)
	writer.WriteAll(rows)
	if err := writer.Error(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	app := rankingApp(t, QueryPlan{Status: "query", SQL: `WITH ranked AS (
  SELECT p.name,s.year,s.on_base_plus_slg,DENSE_RANK() OVER (ORDER BY s.on_base_plus_slg DESC) AS placement
  FROM stats s JOIN players p ON p.player_id=s.player_id WHERE s.year=2026 AND s.pa>=502
 ) SELECT name,year,on_base_plus_slg,placement FROM ranked WHERE placement=1 ORDER BY name`, Seasons: []int{2026}, Columns: []string{"player", "season", "ops", "rank"}}, "Yordan Alvarez and Aaron Judge share first place at the exported OPS of 1.111 in the controlled 2026 snapshot.", path)
	outcome := askRanking(t, app, "Who led MLB in OPS in 2026?")
	want := [][]string{{"Yordan Alvarez", "2026", "1.111", "1"}, {"Aaron Judge", "2026", "1.111", "1"}}
	if !reflect.DeepEqual(outcome.Strategies[0].Table.Rows, want) {
		t.Fatalf("qualified exported-rate ties: %+v", outcome)
	}
}
