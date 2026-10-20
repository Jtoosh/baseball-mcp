package app

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const fixtureIPLimitKey = "test-only-shared-ip-key-at-least-32-bytes"

func hostedIPPost(app http.Handler, token, ip, spoofed string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/questions", strings.NewReader(`{"question":"Who led MLB in OPS?"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", "https://baseball.example")
	req.Header.Set("X-Vercel-Forwarded-For", ip)
	req.Header.Set("X-Forwarded-For", spoofed)
	req.Header.Set("X-Real-IP", spoofed)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	return w
}

func TestNetworkAllowanceSharedAcrossAccountsAndCanonicalAddresses(t *testing.T) {
	store := operationalFixture(t)
	auth, sign, _ := hostedAuthFixture(t)
	svc, _ := testApp(t, PreviewModel{})
	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", Auth: auth, State: store})
	first := sign("user_invited", auth.Issuer, time.Now().Add(time.Hour))
	second := sign("user_second", auth.Issuer, time.Now().Add(time.Hour))
	for i := 0; i < 20; i++ {
		w := hostedIPPost(app, first, "192.0.2.10", "198.51.100.1")
		if !strings.Contains(w.Body.String(), `"kind":"answer"`) {
			t.Fatalf("admission %d: %d %s", i+1, w.Code, w.Body.String())
		}
	}
	w := hostedIPPost(app, second, "::ffff:192.0.2.10", "198.51.100.2")
	if w.Code != 429 {
		t.Fatalf("shared network bypassed allowance: %d %s", w.Code, w.Body.String())
	}
	w = hostedIPPost(app, first, "192.0.2.11", "198.51.100.1")
	if !strings.Contains(w.Body.String(), `"kind":"answer"`) {
		t.Fatalf("independent network denied: %d %s", w.Code, w.Body.String())
	}
}

func TestMissingOrMalformedAuthoritativeNetworkNeverReachesProvider(t *testing.T) {
	store := operationalFixture(t)
	auth, sign, _ := hostedAuthFixture(t)
	svc, _ := testApp(t, waitingErrorModel{"provider reached"})
	var diagnostics strings.Builder
	svc.Logger = log.New(&diagnostics, "", 0)
	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", Auth: auth, State: store})
	token := sign("user_invited", auth.Issuer, time.Now().Add(time.Hour))
	for _, ip := range []string{"", "garbage", "192.0.2.1, 192.0.2.2", "fe80::1%en0", "0.0.0.0", "::", "224.0.0.1"} {
		diagnostics.Reset()
		w := hostedIPPost(app, token, ip, "203.0.113.10")
		if w.Code != 503 || !strings.Contains(w.Body.String(), "temporarily unavailable") || strings.Contains(diagnostics.String(), "interpret question") {
			t.Fatalf("bad authoritative IP reached work: %q %d %s %s", ip, w.Code, w.Body.String(), diagnostics.String())
		}
		if ip != "" && strings.Contains(diagnostics.String(), ip) {
			t.Fatalf("raw network logged: %s", diagnostics.String())
		}
	}
	req := httptest.NewRequest("POST", "/api/questions", strings.NewReader(`{"question":"Who led MLB in OPS?"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Add("X-Vercel-Forwarded-For", "192.0.2.1")
	req.Header.Add("X-Vercel-Forwarded-For", "192.0.2.2")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != 503 {
		t.Fatalf("duplicate authoritative addresses accepted: %d %s", w.Code, w.Body.String())
	}
}

func TestNetworkAllowanceConfigAndProcessingFailuresPersistAfterRestart(t *testing.T) {
	store := operationalFixture(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	auth, sign, _ := hostedAuthFixture(t)
	token := sign("user_invited", auth.Issuer, time.Now().Add(time.Hour))
	svc, _ := testApp(t, waitingErrorModel{"controlled provider failure"})
	svc.Config.HourlyQuestionLimit = 2
	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", Auth: auth, State: store})
	// Invalid JSON, empty questions, and obsolete confirmations consume no slot.
	for _, payload := range []string{`{`, `{"question":""}`, `{"question":"Who led MLB in OPS?","confirmation":"old"}`} {
		hostedPost(app, token, payload)
	}
	first := hostedIPPost(app, token, "192.0.2.10", "")
	if first.Code != 200 || !strings.Contains(first.Body.String(), `"kind":"error"`) {
		t.Fatalf("failed question: %d %s", first.Code, first.Body.String())
	}
	now = now.Add(time.Minute)
	restarted, err := OpenOperationalStore(context.Background(), os.Getenv("TEST_DATABASE_URL"), store.namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	restarted.Now = func() time.Time { return now }
	resumedService, _ := testApp(t, PreviewModel{})
	resumedService.Config.HourlyQuestionLimit = 2
	resumed := NewHostedHandler(resumedService, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", Auth: auth, State: restarted})
	if w := hostedIPPost(resumed, token, "192.0.2.10", ""); !strings.Contains(w.Body.String(), `"kind":"answer"`) {
		t.Fatalf("second admission: %s", w.Body.String())
	}
	w := hostedIPPost(resumed, token, "192.0.2.10", "")
	if w.Code != 429 || w.Header().Get("Retry-After") != "3540" || !strings.Contains(w.Body.String(), "2 questions") {
		t.Fatalf("failed admission not durable: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	now = now.Add(59 * time.Minute)
	if w := hostedIPPost(resumed, token, "192.0.2.10", ""); !strings.Contains(w.Body.String(), `"kind":"answer"`) {
		t.Fatalf("exact rolling boundary did not recover: %s", w.Body.String())
	}
	w = hostedIPPost(resumed, token, "192.0.2.10", "")
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("newer admission did not remain counted: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
}
