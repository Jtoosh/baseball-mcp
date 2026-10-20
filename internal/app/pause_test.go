package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPausedWebsiteRemainsAccessibleAndDeniesNewQuestions(t *testing.T) {
	svc, _ := testApp(t, waitingErrorModel{"provider must not be reached while paused"})
	svc.Config.QuestionsPaused = true
	handler := NewHandler(svc)
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest("GET", "/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `<p id="pause-notice" role="status">Questions are temporarily unavailable</p>`) {
		t.Fatalf("paused page unavailable: %d %s", page.Code, page.Body.String())
	}
	config := httptest.NewRecorder()
	handler.ServeHTTP(config, httptest.NewRequest("GET", "/api/client-config", nil))
	var publicConfig struct {
		QuestionsPaused bool `json:"questions_paused"`
	}
	if err := json.Unmarshal(config.Body.Bytes(), &publicConfig); err != nil || !publicConfig.QuestionsPaused {
		t.Fatalf("paused client config: %s", config.Body.String())
	}
	response := hostedPost(handler, "", `{"question":"Who led MLB in OPS?"}`)
	var got Outcome
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusServiceUnavailable || got.Kind != "error" || got.Message != "Questions are temporarily unavailable" || len(got.Strategies) != 0 {
		t.Fatalf("paused submission: %d %s", response.Code, response.Body.String())
	}
}

func TestHostedPausePreservesAdmissionsAndActiveQuestionsAcrossResume(t *testing.T) {
	store := operationalFixture(t)
	auth, sign, _ := hostedAuthFixture(t)
	token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	started, finish := make(chan struct{}), make(chan struct{})
	running, _ := testApp(t, waitingProviderModel{started, finish})
	paused, _ := testApp(t, waitingErrorModel{"paused request reached provider"})
	paused.Config.QuestionsPaused = true
	resumed, _ := testApp(t, PreviewModel{})
	options := HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", Auth: auth, State: store}
	runningHandler := NewHostedHandler(running, options)
	pausedHandler := NewHostedHandler(paused, options)
	resumedHandler := NewHostedHandler(resumed, options)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- hostedPost(runningHandler, token, `{"question":"Who led MLB in OPS?"}`) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("question did not start")
	}
	defer func() {
		select {
		case <-finish:
		default:
			close(finish)
		}
	}()
	for i := 0; i < 25; i++ {
		response := hostedPost(pausedHandler, token, `{"question":"Who led MLB in OPS?"}`)
		if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "Questions are temporarily unavailable") {
			t.Fatalf("pause failed: %d %s", response.Code, response.Body.String())
		}
	}
	busy := hostedPost(resumedHandler, token, `{"question":"Who led MLB in OPS?"}`)
	if !strings.Contains(busy.Body.String(), "already has a question") {
		t.Fatalf("resume discarded active lease: %s", busy.Body.String())
	}
	close(finish)
	if response := <-done; !strings.Contains(response.Body.String(), `"kind":"unsupported"`) {
		t.Fatalf("pause interrupted admitted question: %s", response.Body.String())
	}
	for i := 0; i < 19; i++ {
		response := hostedPost(resumedHandler, token, `{"question":"Who led MLB in OPS?"}`)
		if !strings.Contains(response.Body.String(), `"kind":"answer"`) {
			t.Fatalf("paused request consumed admission %d: %s", i, response.Body.String())
		}
	}
	response := hostedPost(resumedHandler, token, `{"question":"Who led MLB in OPS?"}`)
	if !strings.Contains(response.Body.String(), "20 questions") {
		t.Fatalf("resume reset admissions: %s", response.Body.String())
	}
}

func TestHostedPauseDeniesNewQuestionsBeforeAuthorizationOrStateWork(t *testing.T) {
	svc, _ := testApp(t, waitingErrorModel{"paused request reached provider"})
	svc.Config.QuestionsPaused = true
	// A paused deployment can return its safe outcome even when authorization or
	// operational storage is unavailable; neither is needed to refuse new work.
	handler := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview"})
	response := hostedPost(handler, "", `{"question":"Who led MLB in OPS?"}`)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"message":"Questions are temporarily unavailable"`) {
		t.Fatalf("pause depended on authorization: %d %s", response.Code, response.Body.String())
	}
}
