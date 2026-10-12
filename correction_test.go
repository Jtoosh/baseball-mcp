package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const correctionLookupSQL = "SELECT year, on_base_plus_slg, pa FROM stats WHERE player_id=676679 AND year=2025"

type correctionModel struct {
	plans      []QueryPlan
	inputs     []PlanInput
	planErrors map[int]error
	answerErr  error
}

func TestBrowserProvidesCorrectionContextPrivately(t *testing.T) {
	calls := 0
	badSQL := "SELECT missing_private_column FROM stats"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		calls++
		plan := QueryPlan{Status: "query", SQL: badSQL, Seasons: []int{2025}, Columns: []string{}}
		var reply any = plan
		if calls == 2 {
			input := payload["input"].(string)
			if !strings.Contains(input, badSQL) || !strings.Contains(input, "no such column") {
				t.Errorf("correction omitted private context: %s", input)
			}
			plan.SQL = correctionLookupSQL
			reply = plan
		}
		if calls == 3 {
			reply = AnswerPresentation{Text: "Luis Vázquez's 2025 OPS was .448 over 53 plate appearances."}
		}
		text, _ := json.Marshal(reply)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "resp_test", "object": "response", "status": "completed", "output": []any{map[string]any{"type": "message", "id": "msg_test", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": string(text), "annotations": []any{}}}}}})
	}))
	defer provider.Close()
	model := &OpenAIModel{Client: openai.NewClient(option.WithAPIKey("test-only"), option.WithBaseURL(provider.URL+"/"), option.WithMaxRetries(0)), ModelName: "test-model"}
	svc, app := testApp(t, model)
	var diagnostics bytes.Buffer
	svc.Logger = log.New(&diagnostics, "", 0)
	body := postQuestion(app).Body.String()
	if calls != 3 || !strings.Contains(body, "0.448") {
		t.Fatalf("correction failed: %s", body)
	}
	if !strings.Contains(diagnostics.String(), "missing_private_column") {
		t.Fatal("diagnostic details were not logged privately")
	}
	for _, private := range []string{badSQL, "missing_private_column", "no such column", "test-only"} {
		if strings.Contains(body, private) {
			t.Fatalf("private detail %q leaked: %s", private, body)
		}
	}
}

func (m *correctionModel) Plan(_ context.Context, input PlanInput) (QueryPlan, error) {
	m.inputs = append(m.inputs, input)
	i := len(m.inputs) - 1
	if i >= len(m.plans) {
		i = len(m.plans) - 1
	}
	return m.plans[i], m.planErrors[len(m.inputs)]
}
func (m *correctionModel) Answer(context.Context, AnswerInput) (AnswerPresentation, error) {
	return AnswerPresentation{Text: "Luis Vázquez's 2025 OPS was .448 over 53 plate appearances in the imported snapshot."}, m.answerErr
}

func TestBrowserStopsAfterSecondQueryFailure(t *testing.T) {
	m := &correctionModel{plans: []QueryPlan{
		{Status: "query", SQL: "SELECT missing_private_column FROM stats", Seasons: []int{2025}},
		{Status: "query", SQL: "SELECT another_missing_column FROM stats", Seasons: []int{2025}},
		{Status: "query", SQL: correctionLookupSQL, Seasons: []int{2025}},
	}}
	_, app := testApp(t, m)
	body := postQuestion(app).Body.String()
	if len(m.inputs) != 2 || !strings.Contains(body, `"kind":"error"`) || strings.Contains(body, `"table"`) || strings.Contains(body, "missing") {
		t.Fatalf("correction budget or privacy failed: %s", body)
	}
}

func TestBrowserRevalidatesCorrectionAndPreservesReadOnlySnapshots(t *testing.T) {
	for _, rejected := range []string{
		"DELETE FROM stats",
		"SELECT year FROM stats; DELETE FROM stats",
		"SELECT year FROM stats; SELECT pa FROM stats",
		"PRAGMA writable_schema=ON",
		"SELECT name FROM sqlite_master",
	} {
		t.Run(rejected, func(t *testing.T) {
			m := &correctionModel{plans: []QueryPlan{
				{Status: "query", SQL: "SELECT missing_private_column FROM stats", Seasons: []int{2025}},
				{Status: "query", SQL: rejected, Seasons: []int{2025}},
				{Status: "query", SQL: correctionLookupSQL, Seasons: []int{2025}},
			}}
			_, app := testApp(t, m)
			body := postQuestion(app).Body.String()
			if len(m.inputs) != 2 || !strings.Contains(body, `"kind":"error"`) || strings.Contains(body, "table") || strings.Contains(body, rejected) {
				t.Fatalf("unsafe correction accepted: %s", body)
			}
			body = postQuestion(app).Body.String()
			if !strings.Contains(body, "0.448") || !strings.Contains(body, "53") {
				t.Fatalf("rejected correction changed the snapshot: %s", body)
			}
			if m.inputs[2].PreviousSQL != "" || m.inputs[2].DatabaseError != "" {
				t.Fatal("previous correction context carried into a fresh question")
			}
		})
	}
}

func TestBrowserCorrectsTimedOutQueryWithFreshExecutionDeadline(t *testing.T) {
	m := &correctionModel{plans: []QueryPlan{
		{Status: "query", SQL: "WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1000000000) SELECT sum(x) FROM n", Seasons: []int{2025}},
		{Status: "query", SQL: correctionLookupSQL, Seasons: []int{2025}},
	}}
	svc, app := testApp(t, m)
	svc.DB.Timeout = 20 * time.Millisecond
	var diagnostics bytes.Buffer
	svc.Logger = log.New(&diagnostics, "", 0)
	start := time.Now()
	body := postQuestion(app).Body.String()
	if time.Since(start) > time.Second || !strings.Contains(body, "0.448") || strings.Contains(body, "interrupted") {
		t.Fatalf("timeout did not recover privately: %s", body)
	}
	if !strings.Contains(diagnostics.String(), "interrupted") {
		t.Fatalf("timeout diagnostic was not recorded: %s", diagnostics.String())
	}
}

func TestBrowserModelFailuresHaveNoTableOrInventedAnswer(t *testing.T) {
	for _, stage := range []string{"initial plan", "correction", "answer"} {
		t.Run(stage, func(t *testing.T) {
			m := &correctionModel{plans: []QueryPlan{{Status: "query", SQL: correctionLookupSQL, Seasons: []int{2025}}}, planErrors: map[int]error{}}
			secret := errors.New("provider raw secret token")
			switch stage {
			case "initial plan":
				m.planErrors[1] = secret
			case "correction":
				m.plans[0].SQL = "SELECT missing_private_column FROM stats"
				m.planErrors[2] = secret
			case "answer":
				m.answerErr = secret
			}
			svc, app := testApp(t, m)
			var diagnostics bytes.Buffer
			svc.Logger = log.New(&diagnostics, "", 0)
			body := postQuestion(app).Body.String()
			if !strings.Contains(body, `"kind":"error"`) || strings.Contains(body, "table") || strings.Contains(body, "0.448") || strings.Contains(body, "secret") {
				t.Fatalf("model failure presented data or leaked details: %s", body)
			}
			if !strings.Contains(diagnostics.String(), secret.Error()) {
				t.Fatal("provider diagnostic missing from private log")
			}
		})
	}
}

func TestBrowserCorrectsFailedQueryOnce(t *testing.T) {
	m := &correctionModel{plans: []QueryPlan{
		{Status: "query", SQL: "SELECT missing_private_column FROM stats", Seasons: []int{2025}},
		{Status: "query", SQL: correctionLookupSQL, Seasons: []int{2025}},
	}}
	_, app := testApp(t, m)
	response := postQuestion(app)
	var outcome Outcome
	if err := json.Unmarshal(response.Body.Bytes(), &outcome); err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != "answer" || outcome.Strategies[0].Table.Rows[0][1] != "0.448" {
		t.Fatalf("correction did not restore the answer: %s", response.Body.String())
	}
}
