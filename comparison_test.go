package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type comparisonModel struct {
	plans              map[string]int
	answers            map[string]int
	planErrors         map[string]error
	answerErrors       map[string]error
	clarify            bool
	clarifyAlternative bool
	needsCorrection    bool
	inputs             []PlanInput
}

func (m *comparisonModel) Plan(_ context.Context, in PlanInput) (QueryPlan, error) {
	if m.plans == nil {
		m.plans = map[string]int{}
	}
	m.plans[in.Strategy]++
	m.inputs = append(m.inputs, in)
	if err := m.planErrors[in.Strategy]; err != nil {
		return QueryPlan{}, err
	}
	if m.clarifyAlternative && in.Strategy == StrategyExamples && m.plans[in.Strategy] == 1 {
		return QueryPlan{Status: "clarification", Message: "Use OPS among qualifying players in 2026.", Seasons: []int{2026}}, nil
	}
	if m.clarify && in.Interpretation == "" {
		return QueryPlan{Status: "clarification", Message: "Use the qualifying hitter with the highest OPS in 2026.", Seasons: []int{2026}}, nil
	}
	if m.clarify {
		return QueryPlan{Status: "query", SQL: "SELECT year,on_base_plus_slg,pa FROM stats WHERE player_id=670541 AND year=2026", Seasons: []int{2026}, Interpretation: in.Interpretation}, nil
	}
	if m.needsCorrection && m.plans[in.Strategy] == 1 {
		return QueryPlan{Status: "query", SQL: "SELECT missing_metric FROM stats", Seasons: []int{2025}}, nil
	}
	return QueryPlan{Status: "query", SQL: "SELECT p.name,s.year,s.on_base_plus_slg,s.pa FROM players p JOIN stats s ON p.player_id=s.player_id WHERE p.player_id=676679 AND s.year=2025", Seasons: []int{2025}, Interpretation: in.Interpretation}, nil
}

func (m *comparisonModel) Answer(_ context.Context, in AnswerInput) (AnswerPresentation, error) {
	if m.answers == nil {
		m.answers = map[string]int{}
	}
	m.answers[in.Strategy]++
	if err := m.answerErrors[in.Strategy]; err != nil {
		return AnswerPresentation{}, err
	}
	return AnswerPresentation{Text: "Luis Vázquez had an OPS of 0.448 over 53 plate appearances in the 2025 snapshot."}, nil
}

func TestBrowserUsesConfiguredDefaultExampleStrategy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"model":"test-model","default_strategy":"examples"}`), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	m := &comparisonModel{}
	svc, app := testApp(t, m)
	svc.Config = config
	got := askPublic(t, app, QuestionRequest{Question: "What was Luis Vázquez's OPS in 2025?"})
	if got.Kind != "answer" || len(got.Strategies) != 1 || got.Strategies[0].Strategy != StrategyExamples || m.plans[StrategyExamples] != 1 || m.answers[StrategyExamples] != 1 {
		t.Fatalf("configured strategy not used: %+v", got)
	}
}

func TestBrowserComparesBothStrategiesWithIndependentResults(t *testing.T) {
	m := &comparisonModel{}
	svc, app := testApp(t, m)
	svc.Config.ComparisonMode = true
	got := askPublic(t, app, QuestionRequest{Question: "What was Luis Vázquez's OPS in 2025?"})
	if got.Kind != "answer" || len(got.Strategies) != 2 {
		t.Fatalf("missing comparison: %+v", got)
	}
	for _, result := range got.Strategies {
		if result.Kind != "answer" || result.Table == nil || result.Table.Rows[0][2] != "0.448" || m.plans[result.Strategy] != 1 || m.answers[result.Strategy] != 1 {
			t.Fatalf("independent result missing: %+v", got)
		}
	}
}

func TestBrowserComparisonPreservesSuccessAfterDefaultPlanningFails(t *testing.T) {
	m := &comparisonModel{planErrors: map[string]error{StrategySchema: errors.New("private SELECT failure")}}
	svc, app := testApp(t, m)
	svc.Config.ComparisonMode = true
	got := askPublic(t, app, QuestionRequest{Question: "What was Luis Vázquez's OPS in 2025?"})
	if got.Kind != "answer" || len(got.Strategies) != 2 || got.Strategies[0].Kind != "error" || got.Strategies[0].Table != nil || got.Strategies[1].Kind != "answer" {
		t.Fatalf("alternative discarded: %+v", got)
	}
}

func TestBrowserComparisonConfirmsBeforeEitherStrategyAnswers(t *testing.T) {
	m := &comparisonModel{clarifyAlternative: true}
	svc, app := testApp(t, m)
	svc.Config.ComparisonMode = true
	got := askPublic(t, app, QuestionRequest{Question: "What was Luis Vázquez's OPS in 2025?"})
	if got.Kind != "clarification" || got.Confirmation == "" || len(m.answers) != 0 {
		t.Fatalf("strategy answered before confirmation: %+v, answers=%v", got, m.answers)
	}
}

func TestBrowserComparisonPreservesSuccessfulAnswerWhenAlternativeSummaryFails(t *testing.T) {
	m := &comparisonModel{answerErrors: map[string]error{StrategyExamples: errors.New("private provider error")}}
	svc, app := testApp(t, m)
	svc.Config.ComparisonMode = true
	got := askPublic(t, app, QuestionRequest{Question: "What was Luis Vázquez's OPS in 2025?"})
	if got.Kind != "answer" || got.Strategies[0].Kind != "answer" || got.Strategies[0].Table == nil || got.Strategies[1].Kind != "error" || got.Strategies[1].Table != nil {
		t.Fatalf("failure isolation: %+v", got)
	}
}

func TestBrowserComparisonHasIndependentCorrectionBudgets(t *testing.T) {
	m := &comparisonModel{needsCorrection: true}
	svc, app := testApp(t, m)
	svc.Config.ComparisonMode = true
	got := askPublic(t, app, QuestionRequest{Question: "What was Luis Vázquez's OPS in 2025?"})
	if got.Kind != "answer" || len(got.Strategies) != 2 {
		t.Fatalf("missing corrected comparison: %+v", got)
	}
	for _, result := range got.Strategies {
		if result.Kind != "answer" || m.plans[result.Strategy] != 2 || m.answers[result.Strategy] != 1 {
			t.Fatalf("incorrect budget: %+v, calls=%v", got, m.plans)
		}
	}
	if len(m.inputs) != 4 || m.inputs[0].PreviousSQL != "" || m.inputs[1].PreviousSQL != "" || m.inputs[2].DatabaseError == "" || m.inputs[3].DatabaseError == "" {
		t.Fatalf("correction context crossed strategies: %+v", m.inputs)
	}
}

func TestBrowserComparisonSharesOneConfirmedInterpretation(t *testing.T) {
	m := &comparisonModel{clarify: true}
	svc, app := testApp(t, m)
	svc.Config.ComparisonMode = true
	question := "Who was the best hitter in 2026?"
	pending := askPublic(t, app, QuestionRequest{Question: question})
	if pending.Kind != "clarification" || len(m.answers) != 0 {
		t.Fatalf("missing shared confirmation: %+v", pending)
	}
	got := askPublic(t, app, QuestionRequest{Question: question, Confirmation: pending.Confirmation})
	if got.Kind != "answer" || len(got.Strategies) != 2 || m.plans[StrategySchema] != 2 || m.plans[StrategyExamples] != 1 {
		t.Fatalf("confirmation repeated per strategy: %+v", got)
	}
	for _, in := range m.inputs[1:] {
		if in.Interpretation != pending.Message {
			t.Fatalf("intent changed: %+v", in)
		}
	}
}

func TestBrowserComparisonUsesDistinctPromptsAndSameConfiguredModel(t *testing.T) {
	var prompts []string
	var models []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Input string `json:"input"`
			Model string `json:"model"`
			Text  struct {
				Format struct {
					Name string `json:"name"`
				} `json:"format"`
			} `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		models = append(models, payload.Model)
		var value any = AnswerPresentation{Text: "Luis Vázquez had 0.448 OPS over 53 plate appearances in 2025."}
		if payload.Text.Format.Name == "query_plan" {
			prompts = append(prompts, payload.Input)
			value = QueryPlan{Status: "query", SQL: "SELECT p.name,s.year,s.on_base_plus_slg,s.pa FROM players p JOIN stats s ON p.player_id=s.player_id WHERE p.player_id=676679 AND s.year=2025", Seasons: []int{2025}, Interpretation: "Look up Luis Vázquez's OPS in 2025."}
		}
		raw, _ := json.Marshal(value)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "resp_comparison", "object": "response", "status": "completed", "output": []any{map[string]any{"type": "message", "id": "msg_comparison", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": string(raw), "annotations": []any{}}}}}})
	}))
	defer provider.Close()
	m := &OpenAIModel{Client: openai.NewClient(option.WithAPIKey("test-only"), option.WithBaseURL(provider.URL+"/"), option.WithMaxRetries(0)), ModelName: "configured-model"}
	svc, app := testApp(t, m)
	svc.Config.ComparisonMode = true
	got := askPublic(t, app, QuestionRequest{Question: "What was Luis Vázquez's OPS in 2025?"})
	if got.Kind != "answer" || len(got.Strategies) != 2 || len(prompts) != 2 || strings.Contains(prompts[0], "Worked question/SQL examples:") || !strings.Contains(prompts[1], "Worked question/SQL examples:") {
		t.Fatalf("prompt comparison missing: %+v", got)
	}
	for _, model := range models {
		if model != "configured-model" {
			t.Fatalf("model changed: %s", model)
		}
	}
	if len(models) != 4 {
		t.Fatalf("unexpected model calls: %d", len(models))
	}
}
