package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	clerk "github.com/clerk/clerk-sdk-go/v2"
	"github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func hostedAuthFixture(t *testing.T) (*ClerkAuth, func(string, string, time.Time) string, *atomic.Bool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	removed := &atomic.Bool{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if removed.Load() {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"errors":[{"code":"resource_not_found","message":"removed"}]}`)
			return
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/sessions/"):
			if strings.HasSuffix(r.URL.Path, "sess_second") {
				fmt.Fprint(w, `{"id":"sess_second","user_id":"user_second","status":"active"}`)
			} else {
				fmt.Fprint(w, `{"id":"sess_test","user_id":"user_invited","status":"active"}`)
			}
		case strings.HasPrefix(r.URL.Path, "/users/"):
			if strings.HasSuffix(r.URL.Path, "user_second") {
				fmt.Fprint(w, `{"id":"user_second","banned":false,"locked":false,"private_metadata":{"baseball_access":true}}`)
			} else {
				fmt.Fprint(w, `{"id":"user_invited","banned":false,"locked":false,"private_metadata":{"baseball_access":true}}`)
			}
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(api.Close)
	auth := NewClerkAuth("sk_test_fixture", "https://clerk.test.accounts.dev", "https://baseball.example", &clerk.ClientConfig{BackendConfig: clerk.BackendConfig{URL: clerk.String(api.URL), Key: clerk.String("sk_test_fixture")}})
	auth.JWK = &clerk.JSONWebKey{Algorithm: "RS256", Key: &key.PublicKey, KeyID: "fixture"}
	sign := func(subject, issuer string, expiry time.Time) string {
		signer, e := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "fixture"))
		if e != nil {
			t.Fatal(e)
		}
		sid := "sess_test"
		if subject == "user_second" {
			sid = "sess_second"
		}
		raw, e := jwt.Signed(signer).Claims(map[string]any{"sub": subject, "iss": issuer, "sid": sid, "azp": "https://baseball.example", "exp": expiry.Unix(), "iat": time.Now().Add(-time.Minute).Unix(), "nbf": time.Now().Add(-time.Minute).Unix()}).CompactSerialize()
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	return auth, sign, removed
}
func hostedPost(app httpHandler, token string, payload string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/questions", strings.NewReader(payload))
	r.Header.Set("Origin", "https://baseball.example")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}
func TestHostedQuestionsRequireCurrentInvitedSession(t *testing.T) {
	auth, sign, removed := hostedAuthFixture(t)
	svc, _ := testApp(t, &scriptedModel{plan: QueryPlan{Status: "unsupported", Message: "Only batting questions are supported."}})
	app := NewHostedHandler(svc, HostedOptions{Mode: "preview", Auth: auth, State: operationalFixture(t)})
	payload := `{"question":"Who pitched best?"}`
	for _, token := range []string{"", sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(-time.Minute)), sign("user_invited", "https://clerk.other.accounts.dev", time.Now().Add(time.Hour)), sign("user_stranger", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))} {
		w := hostedPost(app, token, payload)
		if w.Code != 401 || strings.Contains(w.Body.String(), "strategies") {
			t.Fatalf("unauthorized response: %d %s", w.Code, w.Body.String())
		}
	}
	token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	w := hostedPost(app, token, payload)
	var out Outcome
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Kind != "unsupported" {
		t.Fatalf("invited response: %s", w.Body.String())
	}
	removed.Store(true)
	w = hostedPost(app, token, payload)
	if w.Code != 401 {
		t.Fatalf("removed user allowed: %s", w.Body.String())
	}
	for _, path := range []string{"/mlb.db", "/config.json", "/.private/developer.log"} {
		w = httptest.NewRecorder()
		app.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatalf("private asset reachable %s", path)
		}
	}
}

func operationalFixture(t *testing.T) *OperationalStore {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("REQUIRE_HOSTED_TESTS") == "1" {
			t.Fatal("TEST_DATABASE_URL required")
		}
		t.Skip("set TEST_DATABASE_URL for real PostgreSQL hosted tests")
	}
	store, err := OpenOperationalStore(context.Background(), dsn, "test_"+randomToken())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupOperationalFixture(store); store.Close() })
	return store
}
func TestInvitedUserConfirmsQuestionAcrossInstances(t *testing.T) {
	store := operationalFixture(t)
	auth, sign, _ := hostedAuthFixture(t)
	model := &scriptedModel{plan: QueryPlan{Status: "unsupported", Message: "This controlled question is outside the examples."}}
	svc1, _ := testApp(t, model)
	svc2, _ := testApp(t, model)
	svc1.State = store
	svc2.State = store
	options := HostedOptions{Mode: "preview", Auth: auth, State: store}
	a, b := NewHostedHandler(svc1, options), NewHostedHandler(svc2, options)
	token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	w := hostedPost(a, token, `{"question":"What was Max Muncy's OPS in 2025?"}`)
	var pending Outcome
	json.Unmarshal(w.Body.Bytes(), &pending)
	if pending.Kind != "clarification" || len(pending.Choices) != 2 {
		t.Fatalf("clarification: %s", w.Body.String())
	}
	payload, _ := json.Marshal(QuestionRequest{Question: "What was Max Muncy's OPS in 2025?", Confirmation: pending.Confirmation, Choice: pending.Choices[0]})
	altered := strings.Replace(string(payload), "2025", "2024", 1)
	if w = hostedPost(b, token, altered); !strings.Contains(w.Body.String(), `"kind":"error"`) {
		t.Fatalf("altered question accepted: %s", w.Body.String())
	}
	w = hostedPost(b, token, string(payload))
	if strings.Contains(w.Body.String(), `"kind":"error"`) {
		t.Fatalf("cross-instance confirmation: %s", w.Body.String())
	}
	w = hostedPost(a, token, string(payload))
	if !strings.Contains(w.Body.String(), `"kind":"error"`) {
		t.Fatalf("replay: %s", w.Body.String())
	}
}

func TestHostedSnapshotAndControlledPreviewPreserveReferenceAnswer(t *testing.T) {
	db, cleanup, err := OpenHostedSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	svc := NewQueryService(db, PreviewModel{}, Config{Model: "controlled", DefaultStrategy: StrategySchema}, nil)
	auth, sign, _ := hostedAuthFixture(t)
	app := NewHostedHandler(svc, HostedOptions{Mode: "preview", Auth: auth, State: operationalFixture(t)})
	token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	w := hostedPost(app, token, `{"question":"Who led MLB in OPS?"}`)
	for _, want := range []string{`"kind":"answer"`, "Yordan Alvarez", "1.035", "691", "Controlled preview"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("missing %s: %s", want, w.Body.String())
		}
	}
}

func TestProductionMonthlyAllowanceKeepsUncertainSpendingUntilUTCReset(t *testing.T) {
	store := operationalFixture(t)
	now := time.Date(2026, 10, 31, 23, 59, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		if payload["max_output_tokens"] != float64(2048) || payload["service_tier"] != "default" || payload["store"] != false {
			t.Errorf("unbounded request: %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_fixture","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"status\":\"unsupported\",\"message\":\"Only batting questions are supported.\",\"sql\":\"\",\"seasons\":[],\"columns\":[],\"interpretation\":\"\"}"}]}]}`)
	}))
	t.Cleanup(provider.Close)
	model := &OpenAIModel{Client: openai.NewClient(option.WithAPIKey("test-only"), option.WithBaseURL(provider.URL+"/"), option.WithMaxRetries(0)), ModelName: "gpt-6.1-sol", Budget: &SpendingGuard{State: store}}
	svc, _ := testApp(t, model)
	svc.State = store
	auth, sign, _ := hostedAuthFixture(t)
	app := NewHostedHandler(svc, HostedOptions{Mode: "production", Auth: auth, State: store})
	token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	payload := `{"question":"Who pitched best?"}`
	if w := hostedPost(app, token, payload); !strings.Contains(w.Body.String(), `"kind":"unsupported"`) {
		t.Fatalf("initial: %s", w.Body.String())
	}
	w := hostedPost(app, token, payload)
	if calls.Load() != 1 || !strings.Contains(w.Body.String(), "2026-11-01") || strings.Contains(w.Body.String(), "table") {
		t.Fatalf("budget bypass: calls=%d %s", calls.Load(), w.Body.String())
	}
	w = hostedPost(app, token, `{"question":"What was Max Muncy's OPS in 2025?"}`)
	if !strings.Contains(w.Body.String(), "monthly question allowance") || strings.Contains(w.Body.String(), "confirmation") {
		t.Fatalf("paused allowance still admitted clarification: %s", w.Body.String())
	}
	now = now.Add(2 * time.Minute)
	w = hostedPost(app, token, payload)
	if calls.Load() != 2 || !strings.Contains(w.Body.String(), `"kind":"unsupported"`) {
		t.Fatalf("UTC rollover: %d %s", calls.Load(), w.Body.String())
	}
}

func TestInvitedQuestionsRespectRollingHourlyAdmission(t *testing.T) {
	store := operationalFixture(t)
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	auth, sign, _ := hostedAuthFixture(t)
	token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	svc, _ := testApp(t, PreviewModel{})
	svc.State = store
	app := NewHostedHandler(svc, HostedOptions{Mode: "preview", Auth: auth, State: store})
	for i := 0; i < 20; i++ {
		w := hostedPost(app, token, `{"question":"Who led MLB in OPS?"}`)
		if !strings.Contains(w.Body.String(), `"kind":"answer"`) {
			t.Fatalf("question %d: %s", i+1, w.Body.String())
		}
	}
	w := hostedPost(app, token, `{"question":"Who led MLB in OPS?"}`)
	if !strings.Contains(w.Body.String(), "twenty questions") || strings.Contains(w.Body.String(), "table") {
		t.Fatalf("rate bypass: %s", w.Body.String())
	}
	now = now.Add(time.Hour)
	w = hostedPost(app, token, `{"question":"Who led MLB in OPS?"}`)
	if !strings.Contains(w.Body.String(), `"kind":"answer"`) {
		t.Fatalf("rolling recovery: %s", w.Body.String())
	}
}

type waitingProviderModel struct {
	started chan struct{}
	finish  chan struct{}
}

func (m waitingProviderModel) Plan(ctx context.Context, in PlanInput) (QueryPlan, error) {
	close(m.started)
	select {
	case <-m.finish:
		return QueryPlan{Status: "unsupported", Message: "Only batting questions are supported."}, nil
	case <-ctx.Done():
		return QueryPlan{}, ctx.Err()
	}
}
func (m waitingProviderModel) Answer(context.Context, AnswerInput) (AnswerPresentation, error) {
	return AnswerPresentation{}, errors.New("unexpected answer")
}
func TestInvitedUserHasOnlyOneActiveQuestionAcrossInstances(t *testing.T) {
	store := operationalFixture(t)
	auth, sign, _ := hostedAuthFixture(t)
	token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	started, finish := make(chan struct{}), make(chan struct{})
	svc1, _ := testApp(t, waitingProviderModel{started, finish})
	svc2, _ := testApp(t, PreviewModel{})
	a := NewHostedHandler(svc1, HostedOptions{Mode: "preview", Auth: auth, State: store})
	b := NewHostedHandler(svc2, HostedOptions{Mode: "preview", Auth: auth, State: store})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- hostedPost(a, token, `{"question":"Who led MLB in OPS?"}`) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("question did not start")
	}
	w := hostedPost(b, token, `{"question":"Who led MLB in OPS?"}`)
	if !strings.Contains(w.Body.String(), "already have a question") {
		t.Fatalf("concurrent admission: %s", w.Body.String())
	}
	close(finish)
	<-done
	w = hostedPost(b, token, `{"question":"Who led MLB in OPS?"}`)
	if !strings.Contains(w.Body.String(), `"kind":"answer"`) {
		t.Fatalf("lease not released: %s", w.Body.String())
	}
}
func TestHostedWholeRequestHonorsParentDeadline(t *testing.T) {
	store := operationalFixture(t)
	auth, sign, _ := hostedAuthFixture(t)
	token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	svc, _ := testApp(t, waitingProviderModel{make(chan struct{}), make(chan struct{})})
	app := NewHostedHandler(svc, HostedOptions{Mode: "preview", Auth: auth, State: store})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest("POST", "/api/questions", strings.NewReader(`{"question":"Who led MLB in OPS?"}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "took too long") || strings.Contains(w.Body.String(), "table") {
		t.Fatalf("deadline: %s", w.Body.String())
	}
}

func TestControlledPreviewNeverUsesProductionKeyAndRejectsUnlistedQuestions(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "production-key-must-not-be-used")
	auth, sign, _ := hostedAuthFixture(t)
	svc, _ := testApp(t, PreviewModel{})
	app := NewHostedHandler(svc, HostedOptions{Mode: "preview", Auth: auth, State: operationalFixture(t)})
	token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	for _, q := range []string{"Who led MLB in OPS in 2070?", "Who led MLB in OPS among pitchers?"} {
		payload, _ := json.Marshal(QuestionRequest{Question: q})
		w := hostedPost(app, token, string(payload))
		if !strings.Contains(w.Body.String(), `"kind":"unsupported"`) || strings.Contains(w.Body.String(), "table") {
			t.Fatalf("invented preview answer: %s", w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/api/client-config", nil))
	if !strings.Contains(w.Body.String(), `"mode":"preview"`) || strings.Contains(w.Body.String(), "production-key") {
		t.Fatalf("unsafe client config: %s", w.Body.String())
	}
}

func TestConcurrentInvitedUsersCannotOverbookMonthlyAllowance(t *testing.T) {
	store := operationalFixture(t)
	store.Now = func() time.Time { return time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC) }
	started, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(started)
		<-finish
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"fixture","status":"completed","output":[]}`)
	}))
	t.Cleanup(provider.Close)
	model := func() *OpenAIModel {
		return &OpenAIModel{Client: openai.NewClient(option.WithAPIKey("test-only"), option.WithBaseURL(provider.URL+"/"), option.WithMaxRetries(0)), ModelName: "gpt-6.1-sol", Budget: &SpendingGuard{State: store}}
	}
	svc1, _ := testApp(t, model())
	svc2, _ := testApp(t, model())
	auth, sign, _ := hostedAuthFixture(t)
	a := NewHostedHandler(svc1, HostedOptions{Mode: "production", Auth: auth, State: store})
	b := NewHostedHandler(svc2, HostedOptions{Mode: "production", Auth: auth, State: store})
	token1 := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	token2 := sign("user_second", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- hostedPost(a, token1, `{"question":"Who led MLB in OPS?"}`) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(finish)
		t.Fatal("provider did not start")
	}
	w := hostedPost(b, token2, `{"question":"Who led MLB in OPS?"}`)
	close(finish)
	<-done
	if calls.Load() != 1 || !strings.Contains(w.Body.String(), "monthly question allowance") {
		t.Fatalf("overbooked: calls=%d %s", calls.Load(), w.Body.String())
	}
}
func TestTrustworthyUsagePermitsFurtherQuestionsWithoutRefundingMissingUsage(t *testing.T) {
	store := operationalFixture(t)
	store.Now = func() time.Time { return time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC) }
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_fixture","model":"gpt-6.1-sol","service_tier":"default","status":"completed","usage":{"input_tokens":100,"output_tokens":30,"total_tokens":130,"input_tokens_details":{"cached_tokens":20,"cache_write_tokens":50},"output_tokens_details":{"reasoning_tokens":10}},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"status\":\"unsupported\",\"message\":\"Only batting questions are supported.\",\"sql\":\"\",\"seasons\":[],\"columns\":[],\"interpretation\":\"\"}"}]}]}`)
	}))
	t.Cleanup(provider.Close)
	model := &OpenAIModel{Client: openai.NewClient(option.WithAPIKey("test-only"), option.WithBaseURL(provider.URL+"/"), option.WithMaxRetries(0)), ModelName: "gpt-6.1-sol", Budget: &SpendingGuard{State: store}}
	svc, _ := testApp(t, model)
	auth, sign, _ := hostedAuthFixture(t)
	app := NewHostedHandler(svc, HostedOptions{Mode: "production", Auth: auth, State: store})
	token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	for i := 0; i < 3; i++ {
		w := hostedPost(app, token, `{"question":"Who pitched best?"}`)
		if !strings.Contains(w.Body.String(), `"kind":"unsupported"`) {
			t.Fatalf("trusted usage did not settle: %s", w.Body.String())
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("provider calls=%d", calls.Load())
	}
}

func TestHostedDiagnosticsOmitProviderSecretsAndSQL(t *testing.T) {
	store := operationalFixture(t)
	auth, sign, _ := hostedAuthFixture(t)
	secret := "sk-private-provider-secret SELECT player_id FROM stats postgres://password@private"
	svc, _ := testApp(t, waitingErrorModel{secret})
	var logs bytes.Buffer
	svc.Logger = log.New(&logs, "", 0)
	app := NewHostedHandler(svc, HostedOptions{Mode: "preview", Auth: auth, State: store})
	token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	w := hostedPost(app, token, `{"question":"Who led MLB in OPS?"}`)
	if strings.Contains(logs.String(), secret) || strings.Contains(logs.String(), "player_id") || strings.Contains(w.Body.String(), secret) || !strings.Contains(logs.String(), "class=") {
		t.Fatalf("unsafe diagnostics: %s %s", logs.String(), w.Body.String())
	}
}

type waitingErrorModel struct{ text string }

func (m waitingErrorModel) Plan(context.Context, PlanInput) (QueryPlan, error) {
	return QueryPlan{}, errors.New(m.text)
}
func (m waitingErrorModel) Answer(context.Context, AnswerInput) (AnswerPresentation, error) {
	return AnswerPresentation{}, errors.New(m.text)
}

func TestConfirmationReusesAdmissionAndRejectsAnotherInvitedUser(t *testing.T) {
	store := operationalFixture(t)
	auth, sign, _ := hostedAuthFixture(t)
	svc, _ := testApp(t, PreviewModel{})
	app := NewHostedHandler(svc, HostedOptions{Mode: "preview", Auth: auth, State: store})
	token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	other := sign("user_second", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	w := hostedPost(app, token, `{"question":"What was Max Muncy's OPS in 2025?"}`)
	var pending Outcome
	json.Unmarshal(w.Body.Bytes(), &pending)
	payload, _ := json.Marshal(QuestionRequest{Question: "What was Max Muncy's OPS in 2025?", Confirmation: pending.Confirmation, Choice: pending.Choices[0]})
	w = hostedPost(app, other, string(payload))
	if !strings.Contains(w.Body.String(), `"kind":"error"`) {
		t.Fatalf("different owner accepted: %s", w.Body.String())
	}
	w = hostedPost(app, token, string(payload))
	if strings.Contains(w.Body.String(), `"kind":"error"`) {
		t.Fatalf("confirmation failed: %s", w.Body.String())
	}
	for i := 0; i < 19; i++ {
		w = hostedPost(app, token, `{"question":"Who led MLB in OPS?"}`)
		if !strings.Contains(w.Body.String(), `"kind":"answer"`) {
			t.Fatalf("confirmation double-counted at %d: %s", i, w.Body.String())
		}
	}
	w = hostedPost(app, token, `{"question":"Who led MLB in OPS?"}`)
	if !strings.Contains(w.Body.String(), "twenty questions") {
		t.Fatalf("quota bypass: %s", w.Body.String())
	}
}
func TestExpiredConfirmationAndUnavailableStateFailClosed(t *testing.T) {
	store := operationalFixture(t)
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	auth, sign, _ := hostedAuthFixture(t)
	svc, _ := testApp(t, PreviewModel{})
	app := NewHostedHandler(svc, HostedOptions{Mode: "preview", Auth: auth, State: store})
	token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	w := hostedPost(app, token, `{"question":"What was Max Muncy's OPS in 2025?"}`)
	var pending Outcome
	json.Unmarshal(w.Body.Bytes(), &pending)
	now = now.Add(15 * time.Minute)
	payload, _ := json.Marshal(QuestionRequest{Question: "What was Max Muncy's OPS in 2025?", Confirmation: pending.Confirmation, Choice: pending.Choices[0]})
	w = hostedPost(app, token, string(payload))
	if !strings.Contains(w.Body.String(), "expired") {
		t.Fatalf("expired confirmation accepted: %s", w.Body.String())
	}
	store.Close()
	w = hostedPost(app, token, `{"question":"Who led MLB in OPS?"}`)
	if !strings.Contains(w.Body.String(), `"kind":"error"`) || strings.Contains(w.Body.String(), "table") {
		t.Fatalf("state outage admitted: %s", w.Body.String())
	}
}

func TestExpiredLeaseRecoversWithoutAllowingStaleRelease(t *testing.T) {
	store := operationalFixture(t)
	var clock atomic.Int64
	clock.Store(time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC).UnixNano())
	store.Now = func() time.Time { return time.Unix(0, clock.Load()) }
	auth, sign, _ := hostedAuthFixture(t)
	token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	start1, finish1, start2, finish2 := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	svc1, _ := testApp(t, waitingProviderModel{start1, finish1})
	svc2, _ := testApp(t, waitingProviderModel{start2, finish2})
	svc3, _ := testApp(t, PreviewModel{})
	a := NewHostedHandler(svc1, HostedOptions{Mode: "preview", Auth: auth, State: store})
	b := NewHostedHandler(svc2, HostedOptions{Mode: "preview", Auth: auth, State: store})
	c := NewHostedHandler(svc3, HostedOptions{Mode: "preview", Auth: auth, State: store})
	done1, done2 := make(chan struct{}), make(chan struct{})
	go func() { hostedPost(a, token, `{"question":"Who led MLB in OPS?"}`); close(done1) }()
	select {
	case <-start1:
	case <-time.After(3 * time.Second):
		t.Fatal("first request did not start")
	}
	clock.Add(int64(126 * time.Second))
	go func() { hostedPost(b, token, `{"question":"Who led MLB in OPS?"}`); close(done2) }()
	select {
	case <-start2:
	case <-time.After(3 * time.Second):
		close(finish1)
		t.Fatal("expired lease did not recover")
	}
	close(finish1)
	<-done1
	w := hostedPost(c, token, `{"question":"Who led MLB in OPS?"}`)
	close(finish2)
	<-done2
	if !strings.Contains(w.Body.String(), "already have a question") {
		t.Fatalf("stale owner released new lease: %s", w.Body.String())
	}
}

type deadlineComparisonProvider struct{}

func (deadlineComparisonProvider) Plan(ctx context.Context, in PlanInput) (QueryPlan, error) {
	return QueryPlan{Status: "query", SQL: "SELECT p.name,s.year,s.on_base_plus_slg,s.pa FROM players p JOIN stats s ON p.player_id=s.player_id WHERE p.player_id=676679 AND s.year=2025", Seasons: []int{2025}, Columns: []string{"name", "year", "on_base_plus_slg", "pa"}}, nil
}
func (deadlineComparisonProvider) Answer(ctx context.Context, in AnswerInput) (AnswerPresentation, error) {
	if in.Strategy == StrategyExamples {
		<-ctx.Done()
		return AnswerPresentation{}, ctx.Err()
	}
	return AnswerPresentation{Text: "Luis Vázquez's OPS in 2025 was .448 over 53 plate appearances."}, nil
}
func TestHostedComparisonRetainsCompletedAnswerOnWholeRequestTimeout(t *testing.T) {
	store := operationalFixture(t)
	auth, sign, _ := hostedAuthFixture(t)
	svc, _ := testApp(t, deadlineComparisonProvider{})
	svc.Config.ComparisonMode = true
	app := NewHostedHandler(svc, HostedOptions{Mode: "preview", Auth: auth, State: store})
	token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest("POST", "/api/questions", strings.NewReader(`{"question":"What was Luis Vázquez's OPS in 2025?"}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	var out Outcome
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Kind != "answer" || len(out.Strategies) != 2 || out.Strategies[0].Table == nil || out.Strategies[1].Table != nil || !strings.Contains(out.Strategies[1].Message, "took too long") {
		t.Fatalf("completed comparison lost: %s", w.Body.String())
	}
}

func cleanupOperationalFixture(s *OperationalStore) {
	for _, table := range []string{"baseball_confirmations", "baseball_spending", "baseball_allowances", "baseball_admissions", "baseball_users"} {
		s.pool.Exec(context.Background(), "DELETE FROM "+table+" WHERE namespace=$1", s.namespace)
	}
}
