package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuestionsKeepIndependentProviderBoundsWithoutSpendingGuard(t *testing.T) {
	var calls int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["max_output_tokens"] != float64(2048) || payload["service_tier"] != "default" || payload["store"] != false || payload["model"] != "configured-model" {
			t.Errorf("provider bounds: output=%v tier=%v store=%v model=%v", payload["max_output_tokens"], payload["service_tier"], payload["store"], payload["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"fixture","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"status\":\"unsupported\",\"message\":\"Only batting questions are supported.\",\"sql\":\"\",\"seasons\":[],\"columns\":[],\"interpretation\":\"\",\"lookup_player_ids\":[]}"}]}]}`)
	}))
	defer provider.Close()
	t.Setenv("OPENAI_BASE_URL", provider.URL+"/")
	t.Setenv("OPENAI_API_KEY", "test-only")
	_, handler := testApp(t, NewOpenAIModel("configured-model"))
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", "/api/questions", strings.NewReader(`{"question":"Who pitched best?"}`)))
		if !strings.Contains(w.Body.String(), `"kind":"unsupported"`) {
			t.Fatalf("question denied: %s", w.Body.String())
		}
	}
	if calls != 3 {
		t.Fatalf("provider calls=%d", calls)
	}
}

func TestProviderQuotaFailureIsTemporarySafeAndNeverRetried(t *testing.T) {
	var calls int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"code":"insufficient_quota","type":"insufficient_quota","message":"private provider payload sk-fixture SELECT player_id FROM stats"}}`)
			return
		}
		fmt.Fprint(w, `{"id":"fixture","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"status\":\"unsupported\",\"message\":\"Only batting questions are supported.\",\"sql\":\"\",\"seasons\":[],\"columns\":[],\"interpretation\":\"\",\"lookup_player_ids\":[]}"}]}]}`)
	}))
	defer provider.Close()
	t.Setenv("OPENAI_BASE_URL", provider.URL+"/")
	t.Setenv("OPENAI_API_KEY", "test-only")
	svc, handler := testApp(t, NewOpenAIModel("configured-model"))
	svc.Config.Environment = "production"
	var logs bytes.Buffer
	svc.Logger = log.New(&logs, "", 0)
	ask := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", "/api/questions", strings.NewReader(`{"question":"Who pitched best?"}`)))
		return w
	}
	w := ask()
	if calls != 1 || !strings.Contains(w.Body.String(), `"kind":"error"`) || !strings.Contains(w.Body.String(), "Questions are temporarily unavailable. Please try again later.") {
		t.Fatalf("quota outcome or retry policy: calls=%d %s", calls, w.Body.String())
	}
	if !strings.Contains(logs.String(), "class=provider_unavailable") || strings.Contains(logs.String(), "sk-fixture") || strings.Contains(w.Body.String(), "private provider") || strings.Contains(w.Body.String(), "balance") || strings.Contains(w.Body.String(), "UTC") {
		t.Fatalf("unsafe quota diagnostics: %s %s", logs.String(), w.Body.String())
	}
	if w = ask(); calls != 2 || !strings.Contains(w.Body.String(), `"kind":"unsupported"`) {
		t.Fatalf("provider recovery locked: calls=%d %s", calls, w.Body.String())
	}
}

func TestComparisonCorrectionsAndAnswersKeepProviderBounds(t *testing.T) {
	var plans, corrections, answers int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model           string `json:"model"`
			Input           string `json:"input"`
			MaxOutputTokens int    `json:"max_output_tokens"`
			ServiceTier     string `json:"service_tier"`
			Text            struct {
				Format struct {
					Name string `json:"name"`
				} `json:"format"`
			} `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.MaxOutputTokens != 2048 || payload.ServiceTier != "default" || payload.Model != "configured-model" {
			t.Errorf("unbounded %s call: tokens=%d tier=%q model=%q", payload.Text.Format.Name, payload.MaxOutputTokens, payload.ServiceTier, payload.Model)
		}
		result := map[string]any{}
		if payload.Text.Format.Name == "answer" {
			answers++
			result["text"] = "Luis Vázquez had a .448 OPS over 53 plate appearances in 2025."
		} else {
			plans++
			query := "SELECT unavailable FROM stats"
			if strings.Contains(payload.Input, "Private correction context:") {
				corrections++
				query = "SELECT p.name,s.year,s.on_base_plus_slg,s.pa FROM players p JOIN stats s ON p.player_id=s.player_id WHERE p.player_id=676679 AND s.year=2025"
			}
			result = map[string]any{"status": "query", "sql": query, "seasons": []int{2025}, "columns": []string{"name", "year", "on_base_plus_slg", "pa"}, "interpretation": "Luis Vázquez's 2025 OPS", "message": "", "lookup_player_ids": []int{}}
		}
		raw, _ := json.Marshal(result)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "fixture", "status": "completed", "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": string(raw)}}}}})
	}))
	defer provider.Close()
	t.Setenv("OPENAI_BASE_URL", provider.URL+"/")
	t.Setenv("OPENAI_API_KEY", "test-only")
	svc, handler := testApp(t, NewOpenAIModel("configured-model"))
	svc.Config.ComparisonMode = true
	w := postQuestion(handler)
	var outcome Outcome
	if err := json.Unmarshal(w.Body.Bytes(), &outcome); err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != "answer" || len(outcome.Strategies) != 2 || outcome.Strategies[0].Kind != "answer" || outcome.Strategies[1].Kind != "answer" || plans != 4 || corrections != 2 || answers != 2 {
		t.Fatalf("comparison/correction workflow: plans=%d corrections=%d answers=%d %s", plans, corrections, answers, w.Body.String())
	}
}

func TestLegacyPricingConfigurationDoesNotBlockStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosted.json")
	if err := os.WriteFile(path, []byte(`{"model":"configured-model","environment":"production","pricing_profile":{"model":"old-model","valid_until":"2000-01-01","context_tokens":0}}`), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil || config.Model != "configured-model" {
		t.Fatalf("retired pricing configuration blocked startup: %v", err)
	}
}
