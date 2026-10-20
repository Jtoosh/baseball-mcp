package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalEvaluationBypassesAmbiguousPlayerClarification(t *testing.T) {
	service, app := testApp(t, &comparisonModel{})
	path := t.TempDir() + "/config.json"
	writeInspectionConfig(t, path, `{"model":"test-model","developer_skip_clarification":true}`)
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	service.Config = config
	got := askPublic(t, app, QuestionRequest{Question: "What was Max Muncy's OPS in 2025?"})
	if got.Kind != "answer" || got.Confirmation != "" || len(got.Strategies) != 1 {
		raw, _ := json.Marshal(got)
		t.Fatalf("local model answer blocked: %s", raw)
	}
}

type independentInterpretationModel struct {
	correct bool
	clarify string
}

func (m *independentInterpretationModel) Plan(_ context.Context, in PlanInput) (QueryPlan, error) {
	year := 2024
	meaning := "Rank by home runs"
	if in.Strategy == StrategyExamples {
		year = 2025
		meaning = "Rank by OPS"
	}
	if in.PreviousSQL == "" && in.Interpretation != "" {
		return QueryPlan{}, errors.New("another strategy's interpretation was supplied")
	}
	if in.PreviousSQL != "" && (in.Interpretation != meaning || in.ResolvedIntent == nil || !sameSeasons(in.ResolvedIntent.Seasons, []int{year})) {
		return QueryPlan{}, errors.New("correction lost its strategy context")
	}
	if in.Strategy == m.clarify {
		return QueryPlan{Status: "clarification", Message: "Which metric?"}, nil
	}
	sql := fmt.Sprintf("SELECT year,pa FROM stats WHERE year=%d LIMIT 1", year)
	if m.correct && in.PreviousSQL == "" {
		sql = "SELECT missing_metric FROM stats"
	}
	return QueryPlan{Status: "query", SQL: sql, Interpretation: meaning, Seasons: []int{year}}, nil
}
func (m *independentInterpretationModel) Answer(_ context.Context, in AnswerInput) (AnswerPresentation, error) {
	return AnswerPresentation{Text: fmt.Sprintf("%s in %d.", in.Interpretation, in.Seasons[0])}, nil
}

func TestLocalEvaluationInterpretsStrategiesIndependently(t *testing.T) {
	for _, correct := range []bool{false, true} {
		t.Run(fmt.Sprint(correct), func(t *testing.T) {
			service, app := testApp(t, &independentInterpretationModel{correct: correct})
			service.Config.DeveloperSkipClarification = true
			service.Config.ComparisonMode = true
			service.Config.Listen = "127.0.0.1:8080"
			got := askPublic(t, app, QuestionRequest{Question: "Who was the best hitter?"})
			if got.Kind != "answer" || len(got.Strategies) != 2 || got.Strategies[0].Message != "Rank by home runs in 2024." || got.Strategies[1].Message != "Rank by OPS in 2025." || !sameSeasons(got.Strategies[0].Seasons, []int{2024}) || !sameSeasons(got.Strategies[1].Seasons, []int{2025}) {
				t.Fatalf("independent interpretations lost: %+v", got)
			}
		})
	}
}

func TestLocalEvaluationUnexpectedClarificationReportsRetryAndPreservesOtherAnswer(t *testing.T) {
	service, app := testApp(t, &independentInterpretationModel{clarify: StrategySchema})
	service.Config.DeveloperSkipClarification = true
	service.Config.ComparisonMode = true
	service.Config.Listen = "127.0.0.1:8080"
	got := askPublic(t, app, QuestionRequest{Question: "Who was the best hitter?"})
	if got.Kind != "answer" || got.Confirmation != "" || len(got.Strategies) != 2 || got.Strategies[0].Kind != "error" || !strings.Contains(got.Strategies[0].Message, "try again") || got.Strategies[1].Message != "Rank by OPS in 2025." {
		t.Fatalf("unexpected clarification blocked comparison: %+v", got)
	}
}

func TestProviderChoosesInterpretationWithoutClarification(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		format := payload["text"].(map[string]any)["format"].(map[string]any)
		var reply any = AnswerPresentation{Text: "The selected player had 53 plate appearances in 2025."}
		if format["name"] == "query_plan" {
			instructions := payload["instructions"].(string)
			if !strings.Contains(instructions, "Choose an interpretation independently") || strings.Contains(instructions, "return status clarification") {
				t.Error("direct interpretation prompt still requests clarification")
			}
			schema := format["schema"].(map[string]any)["properties"].(map[string]any)["status"].(map[string]any)
			for _, status := range schema["enum"].([]any) {
				if status == "clarification" {
					t.Error("direct interpretation contract permits clarification")
				}
			}
			reply = QueryPlan{Status: "query", SQL: correctionLookupSQL, Interpretation: "Use OPS in 2025", Seasons: []int{2025}}
		}
		raw, _ := json.Marshal(reply)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "resp_test", "object": "response", "status": "completed", "output": []any{map[string]any{"type": "message", "id": "msg_test", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": string(raw), "annotations": []any{}}}}}})
	}))
	defer provider.Close()
	model := &OpenAIModel{Client: openai.NewClient(option.WithAPIKey("test-only"), option.WithBaseURL(provider.URL+"/"), option.WithMaxRetries(0)), ModelName: "test-model"}
	for _, mode := range []string{"local", "preview", "production"} {
		t.Run(mode, func(t *testing.T) {
			service, app := testApp(t, model)
			service.Config.Environment = mode
			service.Config.DeveloperSkipClarification = mode == "local"
			service.Config.Listen = "127.0.0.1:8080"
			got := askPublic(t, app, QuestionRequest{Question: "Who is the best hitter?"})
			if got.Kind != "answer" {
				t.Fatalf("direct interpretation failed: %+v", got)
			}
		})
	}
}

func TestLocalEvaluationBypassRequiresOptInAndLoopback(t *testing.T) {
	service, app := testApp(t, &comparisonModel{})
	for _, tc := range []struct{ name, config, kind string }{
		{"default off", `{"model":"test-model"}`, "clarification"},
		{"local enabled", `{"model":"test-model","developer_skip_clarification":true}`, "answer"},
		{"network listener", `{"model":"test-model","developer_skip_clarification":true,"listen":"0.0.0.0:8080"}`, "clarification"},
		{"preview", `{"model":"test-model","environment":"preview"}`, "answer"},
		{"production", `{"model":"test-model","environment":"production"}`, "answer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir() + "/config.json"
			writeInspectionConfig(t, path, tc.config)
			c, err := LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			service.Config = c
			got := askPublic(t, app, QuestionRequest{Question: "What was Max Muncy's OPS in 2025?"})
			if got.Kind != tc.kind {
				t.Fatalf("unexpected bypass: %+v", got)
			}
			if len(got.SQLInspection) != 0 {
				t.Fatal("clarification bypass enabled SQL inspection")
			}
		})
	}
}

func TestLocalEvaluationKeepsScopeAndReadOnlyCorrectionLimit(t *testing.T) {
	for _, tc := range []struct {
		name string
		plan QueryPlan
		kind string
	}{
		{"unsupported", QueryPlan{Status: "unsupported", Message: "Pitching is outside the supplied batting snapshots."}, "unsupported"},
		{"unsafe SQL", QueryPlan{Status: "query", SQL: "DELETE FROM stats", Seasons: []int{2025}}, "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &correctionModel{plans: []QueryPlan{tc.plan}}
			service, app := testApp(t, m)
			service.Config.DeveloperSkipClarification = true
			service.Config.Listen = "127.0.0.1:8080"
			got := askPublic(t, app, QuestionRequest{Question: "Who was the best hitter?"})
			if got.Kind != tc.kind {
				t.Fatalf("scope or SQL guard bypassed: %+v", got)
			}
			if tc.kind == "error" && len(m.inputs) != 2 {
				t.Fatal("correction limit changed")
			}
		})
	}
}

func TestLocalEvaluationCorrectedClarificationReturnsRetryError(t *testing.T) {
	m := &correctionModel{plans: []QueryPlan{
		{Status: "query", SQL: "SELECT missing_metric FROM stats", Seasons: []int{2025}},
		{Status: "clarification", Message: "Which metric?"},
	}}
	service, app := testApp(t, m)
	service.Config.DeveloperSkipClarification = true
	service.Config.Listen = "127.0.0.1:8080"
	got := askPublic(t, app, QuestionRequest{Question: "Who was the best hitter?"})
	if got.Kind != "error" || got.Confirmation != "" || !strings.Contains(got.Strategies[0].Message, "try again") || len(m.inputs) != 2 {
		t.Fatalf("corrected clarification not handled: %+v", got)
	}
}

func TestHostedQuestionsInterpretAmbiguityDirectly(t *testing.T) {

	for _, mode := range []string{"preview", "production"} {
		t.Run(mode, func(t *testing.T) {
			model := &correctionModel{plans: []QueryPlan{{Status: "query", SQL: correctionLookupSQL, Interpretation: "Use OPS in 2025", Seasons: []int{2025}}}}
			service, _ := testApp(t, model)
			app := NewHostedHandler(service, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: mode, PublicOrigin: "https://baseball.example", State: operationalFixture(t)})

			for _, question := range []string{"Who was the best hitter?", "What was Max Muncy's OPS in 2025?"} {
				raw, _ := json.Marshal(QuestionRequest{Question: question})
				response := hostedPost(app, string(raw))
				var got Outcome
				if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if response.Code != 200 || got.Kind != "answer" || got.Confirmation != "" || len(got.Choices) != 0 || len(got.SQLInspection) != 0 {
					t.Fatalf("hosted direct answer failed: %+v", got)
				}
			}
			for _, input := range model.inputs {
				if !input.SkipClarification {
					t.Fatal("hosted model still permits clarification")
				}
			}
		})
	}
}
