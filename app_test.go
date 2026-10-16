package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"github.com/ncruces/go-sqlite3"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type scriptedModel struct {
	plan        QueryPlan
	answerErr   error
	answerInput AnswerInput
}

func (m *scriptedModel) Plan(context.Context, PlanInput) (QueryPlan, error) { return m.plan, nil }
func (m *scriptedModel) Answer(_ context.Context, in AnswerInput) (AnswerPresentation, error) {
	m.answerInput = in
	return AnswerPresentation{Text: "Luis Vázquez's OPS in 2025 was .448 over 53 plate appearances."}, m.answerErr
}
func testApp(t *testing.T, m Model, source ...string) (*QueryService, httpHandler) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mlb.db")
	csv := "advanced-mlb-24-26.csv"
	if len(source) > 0 {
		csv = source[0]
	}
	if err := ImportCSV(csv, path); err != nil {
		t.Fatal(err)
	}
	db, err := OpenDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := NewQueryService(db, m, Config{Model: "test-model", DefaultStrategy: StrategySchema}, log.New(io.Discard, "", 0))
	return svc, NewHandler(svc)
}

type httpHandler interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}

func TestBrowserAnswersNonqualifyingPlayerSeasonOPS(t *testing.T) {
	m := &scriptedModel{plan: QueryPlan{Status: "query", SQL: "SELECT p.name, s.year, s.on_base_plus_slg, s.pa FROM players p JOIN stats s ON s.player_id=p.player_id WHERE p.player_id=676679 AND s.year=2025", Seasons: []int{2025}, Columns: []string{"player", "season", "ops", "plate_appearances"}}}
	_, app := testApp(t, m)
	req := httptest.NewRequest("POST", "/api/questions", strings.NewReader(`{"question":"What was Luis Vázquez's OPS in 2025?"}`))
	resp := httptest.NewRecorder()
	app.ServeHTTP(resp, req)
	if resp.Code != 200 {
		t.Fatalf("HTTP %d: %s", resp.Code, resp.Body.String())
	}
	var result Outcome
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Kind != "answer" || len(result.Strategies) != 1 {
		t.Fatalf("unexpected outcome: %+v", result)
	}
	table := result.Strategies[0].Table
	if table == nil || len(table.Rows) != 1 || table.Rows[0][2] != "0.448" || table.Rows[0][3] != "53" {
		t.Fatalf("unexpected table: %+v", table)
	}
	if table.Columns[2] != "OPS" || table.Columns[3] != "Plate appearances" {
		t.Fatalf("schema labels leaked: %+v", table.Columns)
	}
	if len(m.answerInput.Result.Rows) != 1 {
		t.Fatal("model did not receive actual rows")
	}
	for _, secret := range []string{"SELECT", "on_base_plus_slg", "player_id", "676679"} {
		if strings.Contains(resp.Body.String(), secret) {
			t.Fatalf("private detail %q leaked", secret)
		}
	}
}

func TestBrowserCanLookUpPlayerByName(t *testing.T) {
	m := &scriptedModel{plan: QueryPlan{Status: "query", SQL: "SELECT p.name, s.year, s.on_base_plus_slg, s.pa FROM players p JOIN stats s ON s.player_id=p.player_id WHERE lower(p.name) LIKE '%vázquez, luis%' AND s.year=2025", Seasons: []int{2025}}}
	_, app := testApp(t, m)
	r := httptest.NewRecorder()
	app.ServeHTTP(r, httptest.NewRequest("POST", "/api/questions", strings.NewReader(`{"question":"What was Luis Vázquez's OPS in 2025?"}`)))
	if !strings.Contains(r.Body.String(), "0.448") {
		t.Fatalf("name lookup failed: %s", r.Body.String())
	}
}

func postQuestion(app httpHandler) *httptest.ResponseRecorder {
	r := httptest.NewRecorder()
	app.ServeHTTP(r, httptest.NewRequest("POST", "/api/questions", strings.NewReader(`{"question":"What was Luis Vázquez's OPS in 2025?"}`)))
	return r
}
func TestBrowserRejectsNonBaseballAndMultipleStatements(t *testing.T) {
	for _, sql := range []string{
		"DELETE FROM stats", "SELECT name FROM players; SELECT year FROM stats", "PRAGMA table_info(stats)",
		"SELECT name FROM sqlite_master", "SELECT name FROM players; /* innocuous */ DROP TABLE players", "SELECT load_extension('anything')",
		"ATTACH DATABASE '/tmp/private.db' AS private", "SELECT player_id FROM players", "SELECT 'schema detail' AS name FROM players", "SELECT name FROM players\x00; DELETE FROM stats",
	} {
		t.Run(sql, func(t *testing.T) {
			m := &scriptedModel{plan: QueryPlan{Status: "query", SQL: sql, Seasons: []int{2025}}}
			_, app := testApp(t, m)
			r := postQuestion(app)
			var o Outcome
			json.Unmarshal(r.Body.Bytes(), &o)
			if o.Kind != "error" || len(m.answerInput.Result.Rows) != 0 {
				t.Fatalf("query was permitted: %s", r.Body.String())
			}
			if strings.Contains(r.Body.String(), sql) {
				t.Fatal("query leaked")
			}
		})
	}
}
func TestBrowserAcceptsReadOnlyCTEWithComments(t *testing.T) {
	m := &scriptedModel{plan: QueryPlan{Status: "query", SQL: "WITH season AS (SELECT player_id, year, on_base_plus_slg, pa FROM stats WHERE year=2025) SELECT p.name, s.year, s.on_base_plus_slg, s.pa FROM players p JOIN season s ON p.player_id=s.player_id WHERE p.player_id=676679; -- harmless semicolon ;\n", Seasons: []int{2025}}}
	_, app := testApp(t, m)
	if !strings.Contains(postQuestion(app).Body.String(), "0.448") {
		t.Fatal("read-only CTE rejected")
	}
}
func TestBrowserSuppressesTableWhenAnswerGenerationFails(t *testing.T) {
	m := &scriptedModel{plan: QueryPlan{Status: "query", SQL: "SELECT year, on_base_plus_slg, pa FROM stats WHERE player_id=676679 AND year=2025", Seasons: []int{2025}}, answerErr: errors.New("provider raw secret")}
	_, app := testApp(t, m)
	body := postQuestion(app).Body.String()
	if strings.Contains(body, "table") || strings.Contains(body, "secret") || !strings.Contains(body, `"kind":"error"`) {
		t.Fatalf("unexpected failure response: %s", body)
	}
}
func TestBrowserPrivateArtifactsAreNotServed(t *testing.T) {
	_, app := testApp(t, &scriptedModel{})
	for _, path := range []string{"/mlb.db", "/config.json", "/.private/developer.log", "/evaluation/reference-answers.json", "/docs/implementation-spec.md", "/static/../mlb.db", "/llm.go"} {
		r := httptest.NewRecorder()
		app.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code == 200 {
			t.Fatalf("private route served: %s", path)
		}
	}
	r := httptest.NewRecorder()
	app.ServeHTTP(r, httptest.NewRequest("GET", "/", nil))
	if r.Code != 200 || !strings.Contains(r.Body.String(), `id="question-form"`) {
		t.Fatal("browser not served")
	}
}
func TestBrowserCancelsExpensiveExecution(t *testing.T) {
	m := &scriptedModel{plan: QueryPlan{Status: "query", SQL: "WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1000000000) SELECT sum(x) FROM n", Seasons: []int{2025}}}
	svc, app := testApp(t, m)
	svc.DB.Timeout = 20 * time.Millisecond
	start := time.Now()
	body := postQuestion(app).Body.String()
	if time.Since(start) > time.Second {
		t.Fatal("query exceeded controlled deadline")
	}
	if !strings.Contains(body, `"kind":"error"`) {
		t.Fatal("timeout did not produce natural-language error")
	}
}

func TestImportedFixtureMatchesVerifiedSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mlb.db")
	if err := ImportCSV("advanced-mlb-24-26.csv", path); err != nil {
		t.Fatal(err)
	}
	// Artifact checks support the public HTTP fixture; these values come from the
	// independently verified dataset profile, not from importer implementation.
	c, err := sqlite3.OpenFlags(path, sqlite3.OPEN_READONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s, _, err := c.Prepare("SELECT year, count(*), count(CASE WHEN pa >= 502 THEN 1 END) FROM stats GROUP BY year ORDER BY year")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	expected := [][3]int{{2024, 617, 129}, {2025, 632, 145}, {2026, 630, 135}}
	index := 0
	for s.Step() {
		if index >= len(expected) {
			t.Fatal("extra season")
		}
		got := [3]int{s.ColumnInt(0), s.ColumnInt(1), s.ColumnInt(2)}
		if got != expected[index] {
			t.Fatalf("season coverage %v, expected %v", got, expected[index])
		}
		index++
	}
	if index != 3 || s.Err() != nil {
		t.Fatal("missing season")
	}
	s2, _, err := c.Prepare("SELECT count(*), count(CASE WHEN name='Muncy, Max' THEN 1 END) FROM players")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if !s2.Step() || s2.ColumnInt(0) != 857 || s2.ColumnInt(1) != 2 {
		t.Fatal("player identity changed")
	}
	s3, _, err := c.Prepare("SELECT * FROM stats WHERE player_id=664761 AND year=2024")
	if err != nil {
		t.Fatal(err)
	}
	defer s3.Close()
	if !s3.Step() || s3.ColumnCount() != 48 {
		t.Fatal("not every exported metric was imported")
	}
	for i := 0; i < s3.ColumnCount(); i++ {
		if s3.ColumnName(i) == "avg_best_speed" && s3.ColumnFloat(i) != 100.768220847 {
			t.Fatal("advanced metric source precision lost")
		}
	}
}

func TestBrowserReadsDerivedSinglesFromCompatibleExport(t *testing.T) {
	input, err := os.Open("advanced-mlb-24-26.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	raw, err := io.ReadAll(input)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), "\ufeff"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	column := -1
	for i, name := range rows[0] {
		if name == "single" {
			column = i
		}
	}
	if column < 0 {
		t.Fatal("fixture has no singles")
	}
	for i := range rows {
		rows[i] = append(rows[i][:column], rows[i][column+1:]...)
	}
	source := filepath.Join(t.TempDir(), "without-singles.csv")
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(f)
	w.WriteAll(rows)
	if err := w.Error(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "mlb.db")
	if err := ImportCSV(source, path); err != nil {
		t.Fatal(err)
	}
	db, err := OpenDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	m := &scriptedModel{plan: QueryPlan{Status: "query", SQL: "SELECT single, pa FROM stats WHERE player_id=676679 AND year=2025", Seasons: []int{2025}}}
	app := NewHandler(NewQueryService(db, m, Config{DefaultStrategy: StrategySchema}, log.New(io.Discard, "", 0)))
	var result Outcome
	if err := json.Unmarshal(postQuestion(app).Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Kind != "answer" || result.Strategies[0].Table.Rows[0][0] != "6" {
		t.Fatalf("derived singles missing: %+v", result)
	}
}

func TestBrowserUsesConfiguredOpenAIModelAndActualQueryResults(t *testing.T) {
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["model"] != "configured-test-model" {
			t.Errorf("model config ignored: %v", payload["model"])
		}
		calls++
		text := `{"status":"query","sql":"SELECT p.name,s.year,s.on_base_plus_slg,s.pa FROM players p JOIN stats s ON p.player_id=s.player_id WHERE p.player_id=676679 AND s.year=2025","message":"","seasons":[2025],"columns":["player","season","ops","plate_appearances"]}`
		if calls == 2 {
			input := payload["input"].(string)
			if !strings.Contains(input, "0.448") || !strings.Contains(input, "53") {
				t.Errorf("answer model did not get query result: %s", input)
			}
			text = `{"text":"Luis Vázquez's 2025 OPS was .448 over 53 plate appearances in the imported snapshot."}`
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "resp_test", "object": "response", "status": "completed", "output": []any{map[string]any{"type": "message", "id": "msg_test", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}}})
	}))
	defer provider.Close()
	m := &OpenAIModel{Client: openai.NewClient(option.WithAPIKey("test-only"), option.WithBaseURL(provider.URL+"/"), option.WithMaxRetries(0)), ModelName: "configured-test-model"}
	_, app := testApp(t, m)
	body := postQuestion(app).Body.String()
	if calls != 2 || !strings.Contains(body, "0.448") {
		t.Fatalf("SDK workflow failed after %d calls: %s", calls, body)
	}
}
