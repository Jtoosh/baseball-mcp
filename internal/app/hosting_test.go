package app

import (
	"bytes"
	"context"
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

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func hostedPost(app httpHandler, payload string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/questions", strings.NewReader(payload))
	r.Header.Set("Origin", "https://baseball.example")
	r.Header.Set("X-Vercel-Forwarded-For", "192.0.2.10")
	websiteHeaders(r)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
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
func TestAnonymousVisitorAsksIndependentQuestionsAcrossInstances(t *testing.T) {
	store := operationalFixture(t)

	model := &correctionModel{plans: []QueryPlan{{Status: "query", SQL: correctionLookupSQL, Interpretation: "Use OPS in 2025", Seasons: []int{2025}}}}
	svc1, _ := testApp(t, model)
	svc2, _ := testApp(t, model)
	options := HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "production", PublicOrigin: "https://baseball.example", State: store}
	a, b := NewHostedHandler(svc1, options), NewHostedHandler(svc2, options)

	for _, app := range []http.Handler{a, b} {
		w := hostedPost(app, `{"question":"What was Max Muncy's OPS in 2025?"}`)
		var got Outcome
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Kind != "answer" || got.Confirmation != "" || len(got.Choices) != 0 {
			t.Fatalf("direct answer across instances: %s", w.Body.String())
		}
	}
	for _, input := range model.inputs {
		if input.Interpretation != "" || input.ResolvedIntent.PlayerID != 0 || !input.SkipClarification {
			t.Fatalf("new question inherited an interpretation: %+v", input)
		}
	}
}

func TestHostedSnapshotAndControlledPreviewPreserveReferenceAnswer(t *testing.T) {
	db, cleanup, err := OpenHostedSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	svc := NewQueryService(db, PreviewModel{}, Config{Model: "controlled", DefaultStrategy: StrategySchema}, nil)

	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: operationalFixture(t)})

	w := hostedPost(app, `{"question":"Who led MLB in OPS?"}`)
	for _, want := range []string{`"kind":"answer"`, "Yordan Alvarez", "1.035", "691", "Controlled preview"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("missing %s: %s", want, w.Body.String())
		}
	}
}

func TestProductionQuestionsIgnoreLegacySpendingAndPriceExpiry(t *testing.T) {
	store := operationalFixture(t)
	now := time.Date(2026, 11, 30, 23, 59, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	// An existing deployment may have fully spent and uncertain historical records.
	_, err := store.pool.Exec(context.Background(), `INSERT INTO baseball_allowances(namespace,month,spent) VALUES($1,'2026-11',10000000000);`, store.namespace)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(context.Background(), `INSERT INTO baseball_spending(namespace,id,month,amount,settled,created) VALUES($1,'legacy','2026-11',10000000000,false,$2)`, store.namespace, now)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"fixture","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"status\":\"unsupported\",\"message\":\"Only batting questions are supported.\",\"sql\":\"\",\"seasons\":[],\"columns\":[],\"interpretation\":\"\"}"}]}]}`)
	}))
	t.Cleanup(provider.Close)
	model := &OpenAIModel{Client: openai.NewClient(option.WithAPIKey("test-only"), option.WithBaseURL(provider.URL+"/"), option.WithMaxRetries(0)), ModelName: "gpt-6.1-sol"}
	svc, _ := testApp(t, model)

	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "production", PublicOrigin: "https://baseball.example", State: store})
	for i := 0; i < 3; i++ {
		w := hostedPost(app, `{"question":"Who pitched best?"}`)
		if !strings.Contains(w.Body.String(), `"kind":"unsupported"`) {
			t.Fatalf("legacy spending or expired pricing blocked question: %s", w.Body.String())
		}
	}
	restarted, err := OpenOperationalStore(context.Background(), os.Getenv("TEST_DATABASE_URL"), store.namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	restarted.Now = func() time.Time { return now }
	svc, _ = testApp(t, model)
	app = NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "production", PublicOrigin: "https://baseball.example", State: restarted})
	now = now.Add(2 * time.Minute)
	w := hostedPost(app, `{"question":"Who pitched best?"}`)
	if calls.Load() != 4 || !strings.Contains(w.Body.String(), `"kind":"unsupported"`) {
		t.Fatalf("calendar boundary blocked question: calls=%d %s", calls.Load(), w.Body.String())
	}
}

func TestAnonymousQuestionsRespectRollingHourlyAdmission(t *testing.T) {
	store := operationalFixture(t)
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	svc, _ := testApp(t, PreviewModel{})
	svc.State = store
	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: store})
	for i := 0; i < 20; i++ {
		w := hostedPost(app, `{"question":"Who led MLB in OPS?"}`)
		if !strings.Contains(w.Body.String(), `"kind":"answer"`) {
			t.Fatalf("question %d: %s", i+1, w.Body.String())
		}
	}
	w := hostedPost(app, `{"question":"Who led MLB in OPS?"}`)
	if w.Code != 429 || w.Header().Get("Retry-After") != "3600" || !strings.Contains(w.Body.String(), "13:00:00 UTC") || !strings.Contains(w.Body.String(), "20 questions") || strings.Contains(w.Body.String(), "table") {
		t.Fatalf("rate bypass: %s", w.Body.String())
	}
	now = now.Add(time.Hour)
	w = hostedPost(app, `{"question":"Who led MLB in OPS?"}`)
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
func TestAnonymousNetworkHasOnlyOneActiveQuestionAcrossInstances(t *testing.T) {
	store := operationalFixture(t)

	started, finish := make(chan struct{}), make(chan struct{})
	svc1, _ := testApp(t, waitingProviderModel{started, finish})
	svc2, _ := testApp(t, PreviewModel{})
	a := NewHostedHandler(svc1, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: store})
	secondStore, err := OpenOperationalStore(context.Background(), os.Getenv("TEST_DATABASE_URL"), store.namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()
	b := NewHostedHandler(svc2, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: secondStore})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- hostedPost(a, `{"question":"Who led MLB in OPS?"}`) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("question did not start")
	}
	w := hostedPost(b, `{"question":"Who led MLB in OPS?"}`)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), "already has a question") {
		t.Fatalf("concurrent admission: %s", w.Body.String())
	}
	independent := hostedIPPost(b, "192.0.2.11", "")
	if !strings.Contains(independent.Body.String(), `"kind":"answer"`) {
		t.Fatalf("independent network blocked: %d %s", independent.Code, independent.Body.String())
	}
	close(finish)
	<-done
	w = hostedPost(b, `{"question":"Who led MLB in OPS?"}`)
	if !strings.Contains(w.Body.String(), `"kind":"answer"`) {
		t.Fatalf("lease not released: %s", w.Body.String())
	}
}
func TestHostedWholeRequestHonorsParentDeadline(t *testing.T) {
	store := operationalFixture(t)

	svc, _ := testApp(t, waitingProviderModel{make(chan struct{}), make(chan struct{})})
	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: store})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest("POST", "/api/questions", strings.NewReader(`{"question":"Who led MLB in OPS?"}`)).WithContext(ctx)
	websiteHeaders(req)
	req.Header.Set("X-Vercel-Forwarded-For", "192.0.2.10")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "took too long") || strings.Contains(w.Body.String(), "table") {
		t.Fatalf("deadline: %s", w.Body.String())
	}
}

func TestControlledPreviewNeverUsesProductionKeyAndRejectsUnlistedQuestions(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "production-key-must-not-be-used")

	svc, _ := testApp(t, PreviewModel{})
	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: operationalFixture(t)})

	for _, q := range []string{"Who led MLB in OPS in 2070?", "Who led MLB in OPS among pitchers?"} {
		payload, _ := json.Marshal(QuestionRequest{Question: q})
		w := hostedPost(app, string(payload))
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

func TestQuestionsWithReportedUsageRemainAvailable(t *testing.T) {

	store := operationalFixture(t)
	store.Now = func() time.Time { return time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC) }
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_fixture","model":"gpt-6.1-sol","service_tier":"default","status":"completed","usage":{"input_tokens":100,"output_tokens":30,"total_tokens":130,"input_tokens_details":{"cached_tokens":20,"cache_write_tokens":50},"output_tokens_details":{"reasoning_tokens":10}},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"status\":\"unsupported\",\"message\":\"Only batting questions are supported.\",\"sql\":\"\",\"seasons\":[],\"columns\":[],\"interpretation\":\"\"}"}]}]}`)
	}))
	t.Cleanup(provider.Close)
	model := &OpenAIModel{Client: openai.NewClient(option.WithAPIKey("test-only"), option.WithBaseURL(provider.URL+"/"), option.WithMaxRetries(0)), ModelName: "gpt-6.1-sol"}
	svc, _ := testApp(t, model)

	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "production", PublicOrigin: "https://baseball.example", State: store})

	for i := 0; i < 3; i++ {
		w := hostedPost(app, `{"question":"Who pitched best?"}`)
		if !strings.Contains(w.Body.String(), `"kind":"unsupported"`) {
			t.Fatalf("reported usage blocked question: %s", w.Body.String())
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("provider calls=%d", calls.Load())
	}
}

func TestHostedDiagnosticsOmitProviderSecretsAndSQL(t *testing.T) {
	store := operationalFixture(t)

	secret := "sk-private-provider-secret SELECT player_id FROM stats postgres://password@private"
	svc, _ := testApp(t, waitingErrorModel{secret})
	var logs bytes.Buffer
	svc.Logger = log.New(&logs, "", 0)
	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: store})

	w := hostedPost(app, `{"question":"Who led MLB in OPS?"}`)
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

func TestHostedRejectsObsoleteConfirmationBeforeAdmission(t *testing.T) {
	store := operationalFixture(t)

	svc, _ := testApp(t, PreviewModel{})
	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: store})

	// Include a still-valid confirmation created by the previous release.
	ctx := context.WithValue(context.Background(), hostedContextKey{}, &hostedIdentity{User: "user_invited", Admission: "previous-admission"})
	confirmation, err := store.remember(ctx, "Who led MLB in OPS?", ResolvedIntent{Interpretation: "Use OPS"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []QuestionRequest{
		{Confirmation: confirmation}, {Interpretation: "Use OPS"}, {Revision: "Use home runs"}, {Choice: "Max Muncy"},
	} {
		req.Question = "Who led MLB in OPS?"
		payload, _ := json.Marshal(req)
		w := hostedPost(app, string(payload))
		if !strings.Contains(w.Body.String(), `"kind":"error"`) || !strings.Contains(w.Body.String(), "complete question again") {
			t.Fatalf("obsolete clarification accepted: %s", w.Body.String())
		}
	}
	for i := 0; i < 20; i++ {
		w := hostedPost(app, `{"question":"Who led MLB in OPS?"}`)
		if !strings.Contains(w.Body.String(), `"kind":"answer"`) {
			t.Fatalf("invalid request used admission at %d: %s", i, w.Body.String())
		}
	}
	w := hostedPost(app, `{"question":"Who led MLB in OPS?"}`)
	if !strings.Contains(w.Body.String(), "20 questions") {
		t.Fatalf("quota bypass: %s", w.Body.String())
	}
}

func TestUnavailableHostedStateFailsClosed(t *testing.T) {
	store := operationalFixture(t)

	svc, _ := testApp(t, PreviewModel{})
	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: store})

	store.Close()
	w := hostedPost(app, `{"question":"Who led MLB in OPS?"}`)
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"kind":"error"`) || strings.Contains(w.Body.String(), "table") {
		t.Fatalf("state outage admitted: %s", w.Body.String())
	}
}

func TestExpiredLeaseRecoversWithoutAllowingStaleRelease(t *testing.T) {
	store := operationalFixture(t)
	var clock atomic.Int64
	clock.Store(time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC).UnixNano())
	store.Now = func() time.Time { return time.Unix(0, clock.Load()) }

	start1, finish1, start2, finish2 := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	svc1, _ := testApp(t, waitingProviderModel{start1, finish1})
	svc2, _ := testApp(t, waitingProviderModel{start2, finish2})
	svc3, _ := testApp(t, PreviewModel{})
	a := NewHostedHandler(svc1, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: store})
	b := NewHostedHandler(svc2, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: store})
	c := NewHostedHandler(svc3, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: store})
	done1, done2 := make(chan struct{}), make(chan struct{})
	go func() { hostedPost(a, `{"question":"Who led MLB in OPS?"}`); close(done1) }()
	select {
	case <-start1:
	case <-time.After(3 * time.Second):
		t.Fatal("first request did not start")
	}
	clock.Add(int64(126 * time.Second))
	go func() { hostedPost(b, `{"question":"Who led MLB in OPS?"}`); close(done2) }()
	select {
	case <-start2:
	case <-time.After(3 * time.Second):
		close(finish1)
		t.Fatal("expired lease did not recover")
	}
	close(finish1)
	<-done1
	w := hostedPost(c, `{"question":"Who led MLB in OPS?"}`)
	close(finish2)
	<-done2
	if !strings.Contains(w.Body.String(), "already has a question") {
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

	svc, _ := testApp(t, deadlineComparisonProvider{})
	svc.Config.ComparisonMode = true
	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: store})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest("POST", "/api/questions", strings.NewReader(`{"question":"What was Luis Vázquez's OPS in 2025?"}`)).WithContext(ctx)
	websiteHeaders(req)
	req.Header.Set("X-Vercel-Forwarded-For", "192.0.2.10")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	var out Outcome
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Kind != "answer" || len(out.Strategies) != 2 || out.Strategies[0].Table == nil || out.Strategies[1].Table != nil || !strings.Contains(out.Strategies[1].Message, "took too long") {
		t.Fatalf("completed comparison lost: %s", w.Body.String())
	}
}

func cleanupOperationalFixture(s *OperationalStore) {
	for _, table := range []string{"baseball_ip_admissions", "baseball_ip_leases", "baseball_confirmations", "baseball_spending", "baseball_allowances", "baseball_admissions", "baseball_users"} {
		s.pool.Exec(context.Background(), "DELETE FROM "+table+" WHERE namespace=$1", s.namespace)
	}
}
