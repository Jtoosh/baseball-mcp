package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type clarificationModel struct{ scriptedModel }

func (m *clarificationModel) Plan(_ context.Context, in PlanInput) (QueryPlan, error) {
	if in.ResolvedIntent != nil && in.ResolvedIntent.Revision != "" {
		plan := m.plan
		plan.Interpretation = in.ResolvedIntent.Revision
		return plan, nil
	}
	if in.Interpretation == "" {
		return QueryPlan{Status: "clarification", Message: "Find the qualifying MLB hitter with the highest OPS in 2026.", Seasons: []int{2026}}, nil
	}
	return m.plan, nil
}
func askPublic(t *testing.T, app httpHandler, request QuestionRequest) Outcome {
	t.Helper()
	body, _ := json.Marshal(request)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, httptest.NewRequest("POST", "/api/questions", strings.NewReader(string(body))))
	var outcome Outcome
	if err := json.Unmarshal(response.Body.Bytes(), &outcome); err != nil {
		t.Fatal(err)
	}
	return outcome
}
func TestBrowserConfirmsBestGuessBeforeQuerying(t *testing.T) {
	m := &clarificationModel{scriptedModel: scriptedModel{plan: QueryPlan{Status: "query", SQL: "SELECT year, on_base_plus_slg, pa FROM stats WHERE player_id=676679 AND year=2026", Seasons: []int{2026}}}}
	_, app := testApp(t, m)
	pending := askPublic(t, app, QuestionRequest{Question: "Who is the best hitter?"})
	if pending.Kind != "clarification" || pending.Confirmation == "" || !strings.Contains(pending.Message, "OPS") || len(pending.Strategies) != 0 {
		t.Fatalf("missing best guess: %+v", pending)
	}
	answer := askPublic(t, app, QuestionRequest{Question: "Who is the best hitter?", Confirmation: pending.Confirmation})
	if answer.Kind != "answer" || m.answerInput.Interpretation != pending.Message {
		t.Fatalf("confirmation lost intent: %+v", answer)
	}
}

func TestBrowserCanRevisePendingBestGuess(t *testing.T) {
	m := &clarificationModel{scriptedModel: scriptedModel{plan: QueryPlan{Status: "query", SQL: "SELECT year, home_run, pa FROM stats WHERE player_id=676679 AND year=2026", Seasons: []int{2026}}}}
	_, app := testApp(t, m)
	pending := askPublic(t, app, QuestionRequest{Question: "Who is the best hitter?"})
	answer := askPublic(t, app, QuestionRequest{Question: "Who is the best hitter?", Confirmation: pending.Confirmation, Revision: "Use home runs instead of OPS."})
	if answer.Kind != "answer" || !strings.Contains(m.answerInput.Interpretation, "home runs") {
		t.Fatalf("revision lost: %+v", answer)
	}
}

type playerChoiceModel struct{ scriptedModel }

func (m *playerChoiceModel) Plan(_ context.Context, in PlanInput) (QueryPlan, error) {
	id := int64(571970)
	if in.ResolvedIntent != nil && in.ResolvedIntent.PlayerID != 0 {
		id = in.ResolvedIntent.PlayerID
	}
	return QueryPlan{Status: "query", SQL: fmt.Sprintf("SELECT year,on_base_plus_slg,pa FROM stats WHERE player_id=%d AND year=2026", id), Seasons: []int{2026}}, nil
}
func TestBrowserDistinguishesSameNamePlayersWithoutPrivateIDs(t *testing.T) {
	_, app := testApp(t, &playerChoiceModel{})
	for _, example := range []struct{ age, ops, pa string }{{"35", "0.838", "561"}, {"23", "0.683", "284"}} {
		pending := askPublic(t, app, QuestionRequest{Question: "What was Max Muncy's OPS in 2026?"})
		if pending.Kind != "clarification" || len(pending.Choices) != 2 {
			t.Fatalf("silently chose player: %+v", pending)
		}
		body, _ := json.Marshal(pending)
		for _, private := range []string{"571970", "691777", "player_id", "SELECT"} {
			if strings.Contains(string(body), private) {
				t.Fatalf("private distinction leaked: %s", body)
			}
		}
		choice := ""
		for _, c := range pending.Choices {
			if strings.Contains(c, "age "+example.age) {
				choice = c
			}
		}
		answer := askPublic(t, app, QuestionRequest{Question: "What was Max Muncy's OPS in 2026?", Confirmation: pending.Confirmation, Choice: choice})
		if answer.Kind != "answer" || answer.Strategies[0].Table.Rows[0][1] != example.ops || answer.Strategies[0].Table.Rows[0][2] != example.pa {
			t.Fatalf("wrong Max Muncy: %+v", answer)
		}
	}
}

func TestBrowserRejectsForgedMismatchedAndReusedConfirmations(t *testing.T) {
	m := &clarificationModel{scriptedModel: scriptedModel{plan: QueryPlan{Status: "query", SQL: "SELECT year,pa FROM stats WHERE player_id=676679 AND year=2026", Seasons: []int{2026}}}}
	_, app := testApp(t, m)
	pending := askPublic(t, app, QuestionRequest{Question: "Who is the best hitter?"})
	for _, req := range []QuestionRequest{
		{Question: "Who is the best hitter?", Confirmation: "invented"},
		{Question: "A different question", Confirmation: pending.Confirmation},
		{Question: "Who is the best hitter?", Confirmation: pending.Confirmation, Interpretation: "SELECT secret FROM private"},
	} {
		if got := askPublic(t, app, req); got.Kind != "error" {
			t.Fatalf("untrusted confirmation accepted: %+v", got)
		}
	}
	if got := askPublic(t, app, QuestionRequest{Question: "Who is the best hitter?", Confirmation: pending.Confirmation}); got.Kind != "answer" {
		t.Fatalf("valid confirmation rejected: %+v", got)
	}
	if got := askPublic(t, app, QuestionRequest{Question: "Who is the best hitter?", Confirmation: pending.Confirmation}); got.Kind != "error" {
		t.Fatalf("confirmation reused: %+v", got)
	}
}

type scopeModel struct{ scriptedModel }

func (m *scopeModel) Plan(_ context.Context, in PlanInput) (QueryPlan, error) {
	if !strings.Contains(in.Schema, "CREATE TABLE") {
		return QueryPlan{}, fmt.Errorf("missing scope context")
	}
	return QueryPlan{Status: "unsupported", Message: "These imported snapshots cover MLB batting from 2024 through 2026, combined across teams and leagues. Please ask about a batting statistic in those seasons."}, nil
}
func TestBrowserExplainsUnsupportedScopeWithoutQueryOrTable(t *testing.T) {
	_, app := testApp(t, &scopeModel{})
	for _, question := range []string{"Who led MLB in ERA in 2026?", "Who hit the most home runs in 2023?", "What was the Dodgers team OPS in 2026?", "Who led the American League in home runs in 2026?"} {
		got := askPublic(t, app, QuestionRequest{Question: question})
		if got.Kind != "unsupported" || !strings.Contains(got.Message, "batting") || len(got.Strategies) != 0 {
			t.Fatalf("bad unsupported result: %+v", got)
		}
	}
}

type independentModel struct{ scriptedModel }

func (m *independentModel) Plan(_ context.Context, in PlanInput) (QueryPlan, error) {
	if strings.Contains(in.Question, "his") && (in.Interpretation != "" || in.ResolvedIntent.PlayerID != 0) {
		return m.plan, nil
	}
	if strings.Contains(in.Question, "his") {
		return QueryPlan{Status: "clarification", Message: "Which player should I look up?", Seasons: []int{2026}}, nil
	}
	return m.plan, nil
}
func TestBrowserCompletedAndPendingQuestionsDoNotSupplyConversationMemory(t *testing.T) {
	m := &independentModel{scriptedModel: scriptedModel{plan: QueryPlan{Status: "query", SQL: "SELECT year,home_run,pa FROM stats WHERE player_id=571970 AND year=2026", Seasons: []int{2026}}}}
	_, app := testApp(t, m)
	p := askPublic(t, app, QuestionRequest{Question: "What was Max Muncy's OPS in 2026?"})
	if got := askPublic(t, app, QuestionRequest{Question: "What were his home runs?"}); got.Kind != "clarification" {
		t.Fatalf("pending question leaked context: %+v", got)
	}
	got := askPublic(t, app, QuestionRequest{Question: "What was Max Muncy's OPS in 2026?", Confirmation: p.Confirmation, Choice: p.Choices[0]})
	if got.Kind != "answer" {
		t.Fatalf("lookup failed: %+v", got)
	}
	if got := askPublic(t, app, QuestionRequest{Question: "What were his home runs?"}); got.Kind != "clarification" {
		t.Fatalf("completed question leaked context: %+v", got)
	}
}

func TestBrowserSDKSupportsClarificationAndScopeExplanations(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		input, _ := payload["input"].(string)
		text := `{"status":"clarification","interpretation":"Rank qualifying hitters by OPS in 2026.","sql":"","message":"Rank qualifying hitters by OPS in 2026.","seasons":[2026],"columns":[]}`
		if !strings.Contains(input, "CREATE TABLE") && strings.Contains(input, `"table"`) {
			text = `{"text":"The imported 2026 snapshot has the requested plate appearances."}`
		} else if strings.Contains(input, "ERA") {
			if !strings.Contains(input, "CREATE TABLE") {
				t.Error("unsupported question lacked schema context")
			}
			text = `{"status":"unsupported","interpretation":"","sql":"","message":"These snapshots contain batting statistics; pitching ERA is outside the available scope.","seasons":[],"columns":[]}`
		} else if strings.Contains(input, "Resolved interpretation: Rank qualifying hitters") {
			text = `{"status":"query","interpretation":"Rank qualifying hitters by OPS in 2026.","sql":"SELECT year,pa FROM stats WHERE player_id=676679 AND year=2026","message":"","seasons":[2026],"columns":["season","plate_appearances"]}`
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "resp_test", "object": "response", "status": "completed", "output": []any{map[string]any{"type": "message", "id": "msg_test", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}}})
	}))
	defer provider.Close()
	m := &OpenAIModel{Client: openai.NewClient(option.WithAPIKey("test-only"), option.WithBaseURL(provider.URL+"/"), option.WithMaxRetries(0)), ModelName: "test"}
	_, app := testApp(t, m)
	p := askPublic(t, app, QuestionRequest{Question: "Who is the best hitter?"})
	if p.Kind != "clarification" {
		t.Fatalf("SDK clarification failed: %+v", p)
	}
	if got := askPublic(t, app, QuestionRequest{Question: "Who is the best hitter?", Confirmation: p.Confirmation}); got.Kind != "answer" {
		t.Fatalf("SDK confirmation failed: %+v", got)
	}
	if got := askPublic(t, app, QuestionRequest{Question: "Who led in ERA?"}); got.Kind != "unsupported" || !strings.Contains(got.Message, "pitching") {
		t.Fatalf("SDK scope failed: %+v", got)
	}
}

func TestBrowserOneConfirmationCanBeConsumedOnlyOnceConcurrently(t *testing.T) {
	m := &clarificationModel{scriptedModel: scriptedModel{plan: QueryPlan{Status: "query", SQL: "SELECT year,pa FROM stats WHERE player_id=676679 AND year=2026", Seasons: []int{2026}}}}
	_, app := testApp(t, m)
	p := askPublic(t, app, QuestionRequest{Question: "Who is the best hitter?"})
	outcomes := make(chan Outcome, 8)
	for i := 0; i < 8; i++ {
		go func() {
			outcomes <- askPublic(t, app, QuestionRequest{Question: "Who is the best hitter?", Confirmation: p.Confirmation})
		}()
	}
	answers := 0
	for i := 0; i < 8; i++ {
		if got := <-outcomes; got.Kind == "answer" {
			answers++
		} else if got.Kind != "error" {
			t.Errorf("unexpected concurrent outcome: %+v", got)
		}
	}
	if answers != 1 {
		t.Fatalf("confirmation produced %d answers", answers)
	}
}

func TestBrowserExplicitPlayerDistinctionDoesNotNeedConfirmation(t *testing.T) {
	_, app := testApp(t, &playerChoiceModel{})
	got := askPublic(t, app, QuestionRequest{Question: "What was Max Muncy's OPS in 2026, the player age 23?"})
	if got.Kind != "answer" || got.Strategies[0].Table.Rows[0][1] != "0.683" {
		t.Fatalf("explicit player distinction was ignored: %+v", got)
	}
}

func TestBrowserSuppressesPrivateUnsupportedExplanation(t *testing.T) {
	_, app := testApp(t, &scriptedModel{plan: QueryPlan{Status: "unsupported", Message: "Max Muncy is identified by 571970 and 691777."}})
	got := askPublic(t, app, QuestionRequest{Question: "Tell me Max Muncy's pitching statistics, age 23."})
	if got.Kind != "error" || strings.Contains(got.Message, "571970") || strings.Contains(got.Message, "691777") {
		t.Fatalf("private IDs leaked: %+v", got)
	}
}
