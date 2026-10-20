package app

import (
	"bytes"
	"context"
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

func TestLocalQuestionsPreserveProviderDefaultsWithoutSpendingGuard(t *testing.T) {
	var calls int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if _, present := payload["max_output_tokens"]; present {
			t.Errorf("local question unexpectedly caps provider output: %v", payload["max_output_tokens"])
		}
		if _, present := payload["service_tier"]; present {
			t.Errorf("local question unexpectedly selects provider tier: %v", payload["service_tier"])
		}
		if payload["store"] != false || payload["model"] != "configured-model" {
			t.Errorf("provider settings: store=%v model=%v", payload["store"], payload["model"])
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

func TestHostedComparisonCorrectionsAndAnswersKeepProviderBounds(t *testing.T) {
	var plans, corrections, answers int
	provider := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})
	handler := capturedProductionApp(t, provider, true)
	w := hostedPost(handler, `{"question":"What was Luis Vázquez's OPS in 2025?"}`)
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

// Capture the SDK transport after the real hosted composition, including its
// pinned provider origin. No request is forwarded to a paid provider.
type capturedProviderTransport struct{ handler http.Handler }

func (c capturedProviderTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "api.openai.com" || r.URL.Path != "/v1/responses" {
		return nil, fmt.Errorf("unexpected provider endpoint: %s", r.URL)
	}
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	return w.Result(), nil
}

func capturedProductionApp(t *testing.T, provider http.Handler, comparison bool) http.Handler {
	t.Helper()
	// Use the real PostgreSQL fixture prerequisite, then clean the production
	// namespace used by startup as well as its independent fixture namespace.
	operationalFixture(t)
	state, err := OpenOperationalStore(context.Background(), os.Getenv("TEST_DATABASE_URL"), "production")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupOperationalFixture(state); state.Close() })
	original := http.DefaultTransport
	http.DefaultTransport = capturedProviderTransport{provider}
	t.Cleanup(func() { http.DefaultTransport = original })
	t.Setenv("OPENAI_API_KEY", "test-only")
	// Startup must pin its provider endpoint even if local overrides are set.
	t.Setenv("OPENAI_BASE_URL", "https://must-not-be-used.example/")
	handler, cleanup, err := startHosted(Config{
		Environment: "production", Model: "configured-model", DefaultStrategy: StrategySchema,
		ComparisonMode: comparison, PublicOrigin: "https://baseball.example",
		ipLimitKey: fixtureIPLimitKey, operationalDSN: os.Getenv("TEST_DATABASE_URL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	return handler
}
