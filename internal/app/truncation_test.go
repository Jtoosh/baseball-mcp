package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type partialResultModel struct {
	plan         QueryPlan
	answerText   string
	answerErr    error
	answerInputs []AnswerInput
}

func (m *partialResultModel) Plan(context.Context, PlanInput) (QueryPlan, error) { return m.plan, nil }
func (m *partialResultModel) Answer(_ context.Context, input AnswerInput) (AnswerPresentation, error) {
	m.answerInputs = append(m.answerInputs, input)
	return AnswerPresentation{Text: m.answerText}, m.answerErr
}

func askPartialQuestion(t *testing.T, app httpHandler) (Outcome, string) {
	t.Helper()
	response := httptest.NewRecorder()
	app.ServeHTTP(response, httptest.NewRequest("POST", "/api/questions", strings.NewReader(`{"question":"Show the batting records for all seasons."}`)))
	if response.Code != 200 {
		t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
	}
	var outcome Outcome
	if err := json.Unmarshal(response.Body.Bytes(), &outcome); err != nil {
		t.Fatal(err)
	}
	return outcome, response.Body.String()
}

func TestBrowserAnswersFirst500RowsAndDisclosesPartialResults(t *testing.T) {
	m := &partialResultModel{
		plan: QueryPlan{Status: "query", SQL: "SELECT year, pa FROM stats ORDER BY year, player_id LIMIT 501", Seasons: []int{2024, 2025, 2026}},
		// Deliberately omit a model-authored disclosure: the server must preserve
		// the public partial-result contract independently of the model's wording.
		answerText: "These batting records come from the imported season snapshots.",
	}
	_, app := testApp(t, m)
	outcome, body := askPartialQuestion(t, app)
	if outcome.Kind != "answer" || len(outcome.Strategies) != 1 {
		t.Fatalf("expected partial answer: %s", body)
	}
	result := outcome.Strategies[0]
	if result.Kind != "answer" || !result.Partial {
		t.Fatalf("partial status missing: %+v", result)
	}
	if result.Table == nil || len(result.Table.Rows) != 500 {
		t.Fatalf("expected exactly 500 public rows: %+v", result.Table)
	}
	if len(m.answerInputs) != 1 || len(m.answerInputs[0].Result.Rows) != 500 || !m.answerInputs[0].Result.Truncated {
		t.Fatalf("overflow row or missing context reached model: %+v", m.answerInputs)
	}
	for _, phrase := range []string{"partial", "first 500", "narrow"} {
		if !strings.Contains(strings.ToLower(result.Message), phrase) {
			t.Fatalf("missing disclosure %q: %s", phrase, result.Message)
		}
	}
	if result.Table.Columns[0] != "Season" || result.Table.Columns[1] != "Plate appearances" {
		t.Fatalf("unfriendly table: %+v", result.Table.Columns)
	}
}

func TestBrowserSendsPartialContextAndInstructionsToAnswerProvider(t *testing.T) {
	answers := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		format := payload["text"].(map[string]any)["format"].(map[string]any)["name"]
		text := `{"status":"query","sql":"SELECT year,pa FROM stats ORDER BY year,player_id LIMIT 501","message":"","seasons":[2024,2025,2026],"columns":["year","pa"]}`
		if format == "answer" {
			answers++
			var input struct {
				Partial bool              `json:"partial"`
				Table   PresentationTable `json:"table"`
			}
			if err := json.Unmarshal([]byte(payload["input"].(string)), &input); err != nil {
				t.Error(err)
			}
			if !input.Partial || len(input.Table.Rows) != 500 {
				t.Errorf("bad bounded provider input: partial=%t rows=%d", input.Partial, len(input.Table.Rows))
			}
			instructions := strings.ToLower(payload["instructions"].(string))
			for _, phrase := range []string{"first 500", "partial", "narrow"} {
				if !strings.Contains(instructions, phrase) {
					t.Errorf("provider instructions omit %q", phrase)
				}
			}
			text = `{"text":"The returned batting records reflect imported season snapshots."}`
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "resp_test", "object": "response", "status": "completed", "output": []any{map[string]any{"type": "message", "id": "msg_test", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}}})
	}))
	defer provider.Close()
	m := &OpenAIModel{Client: openai.NewClient(option.WithAPIKey("test-only"), option.WithBaseURL(provider.URL+"/"), option.WithMaxRetries(0)), ModelName: "test-model"}
	_, app := testApp(t, m)
	result, body := askPartialQuestion(t, app)
	if result.Kind != "answer" || answers != 1 {
		t.Fatalf("expected SDK partial answer: %s", body)
	}
}

func TestBrowserKeepsUntruncatedBoundariesAndEmptyResultsNormal(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		rows      int
	}{
		{"499 rows", "SELECT year, pa FROM stats ORDER BY year, player_id LIMIT 499", 499},
		{"exactly 500 rows", "SELECT year, pa FROM stats ORDER BY year, player_id LIMIT 500", 500},
		{"no matches", "SELECT year, pa FROM stats WHERE pa < 0", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &partialResultModel{plan: QueryPlan{Status: "query", SQL: tc.sql, Seasons: []int{2024, 2025, 2026}}, answerText: "The result reflects imported season snapshots."}
			_, app := testApp(t, m)
			outcome, body := askPartialQuestion(t, app)
			if outcome.Kind != "answer" || len(outcome.Strategies) != 1 {
				t.Fatalf("normal result failed: %s", body)
			}
			result := outcome.Strategies[0]
			if result.Partial || strings.Contains(strings.ToLower(result.Message), "partial") || strings.Contains(result.Message, "narrow") {
				t.Fatalf("false overflow: %s", body)
			}
			if result.Table == nil || len(result.Table.Rows) != tc.rows {
				t.Fatalf("expected %d public rows: %+v", tc.rows, result.Table)
			}
			if len(m.answerInputs) != 1 || m.answerInputs[0].Result.Truncated || len(m.answerInputs[0].Result.Rows) != tc.rows {
				t.Fatalf("unexpected model boundary: %+v", m.answerInputs)
			}
		})
	}
}

func TestBrowserCountsAllRelevantRecordsDespiteResultCap(t *testing.T) {
	// These independent snapshot totals are documented in docs/data-profile.md.
	for _, tc := range []struct{ name, sql, count string }{
		{"all player-seasons", "SELECT count(*) FROM stats", "1879"},
		{"all qualifying player-seasons", "SELECT count(*) FROM stats WHERE pa >= 502", "409"},
		{"2025 qualifying player-seasons", "SELECT count(*) FROM stats WHERE pa >= 502 AND year=2025", "145"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &partialResultModel{plan: QueryPlan{Status: "query", SQL: tc.sql, Seasons: []int{2024, 2025, 2026}, Columns: []string{"player_count"}}, answerText: "The count covers the imported season snapshots."}
			_, app := testApp(t, m)
			outcome, body := askPartialQuestion(t, app)
			if outcome.Kind != "answer" || len(outcome.Strategies) != 1 {
				t.Fatalf("count failed: %s", body)
			}
			result := outcome.Strategies[0]
			if result.Partial || result.Table == nil || len(result.Table.Rows) != 1 || result.Table.Rows[0][0] != tc.count {
				t.Fatalf("count did not cover all records: %s", body)
			}
			if len(m.answerInputs) != 1 || m.answerInputs[0].Result.Truncated || len(m.answerInputs[0].Result.Rows) != 1 {
				t.Fatalf("wrong count input: %+v", m.answerInputs)
			}
		})
	}
}

func TestBrowserSuppressesPartialTableWhenAnswerGenerationFails(t *testing.T) {
	m := &partialResultModel{plan: QueryPlan{Status: "query", SQL: "SELECT year,pa FROM stats ORDER BY year,player_id LIMIT 501", Seasons: []int{2024, 2025, 2026}}, answerErr: errors.New("private provider failure")}
	_, app := testApp(t, m)
	outcome, body := askPartialQuestion(t, app)
	if outcome.Kind != "error" || strings.Contains(body, "table") || strings.Contains(body, "private provider failure") {
		t.Fatalf("partial answer failure exposed table or internals: %s", body)
	}
	if len(m.answerInputs) != 1 || len(m.answerInputs[0].Result.Rows) != 500 || !m.answerInputs[0].Result.Truncated {
		t.Fatalf("answer was not attempted with bounded context: %+v", m.answerInputs)
	}
}
