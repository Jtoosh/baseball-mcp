package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnonymousHostedWebsiteFirstUse(t *testing.T) {
	svc, _ := testApp(t, PreviewModel{})
	svc.Config.PublicOrigin = "https://baseball.example"
	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", State: operationalFixture(t)})
	w := hostedPost(app, `{"question":"Who led MLB in OPS?"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"kind":"answer"`) {
		t.Fatalf("anonymous website question: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(w.Body.String(), "sign-in") || strings.Contains(w.Body.String(), "Sign in") || !strings.Contains(w.Body.String(), `id="question-form"`) {
		t.Fatalf("anonymous form unavailable: %s", w.Body.String())
	}
}

func websiteHeaders(r *http.Request) {
	r.Header.Set("Origin", "https://baseball.example")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Sec-Fetch-Mode", "same-origin")
	r.Header.Set("Sec-Fetch-Dest", "empty")
	r.Header.Set("X-Baseball-Website", "1")
}

func TestControlledPreviewShowsNoticeBeforeBrowserInitialization(t *testing.T) {
	svc, _ := testApp(t, PreviewModel{})
	app := NewHostedHandler(svc, HostedOptions{Mode: "preview"})
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(w.Body.String(), `<p id="preview-notice">Controlled preview: test responses only. No paid model calls.</p>`) {
		t.Fatal("controlled preview notice is hidden on the first response")
	}
}

func TestWebsiteBoundaryRejectsInvalidSubmissionsWithoutUsingAllowance(t *testing.T) {
	svc, _ := testApp(t, PreviewModel{})
	svc.Config.HourlyQuestionLimit = 1
	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: operationalFixture(t)})
	for _, change := range []struct{ header, value string }{
		{"Origin", ""}, {"Origin", "null"}, {"Origin", "https://other.example"},
		{"Sec-Fetch-Site", ""}, {"Sec-Fetch-Site", "cross-site"}, {"Sec-Fetch-Site", "same-site"},
		{"Sec-Fetch-Mode", ""}, {"Sec-Fetch-Mode", "navigate"}, {"Sec-Fetch-Dest", ""},
		{"X-Baseball-Website", ""}, {"Content-Type", ""}, {"Content-Type", "text/plain"},
		{"Content-Type", "application/x-www-form-urlencoded"},
	} {
		r := httptest.NewRequest("POST", "/api/questions", strings.NewReader(`{"question":"Who led MLB in OPS?"}`))
		websiteHeaders(r)
		r.Header.Set("X-Vercel-Forwarded-For", "192.0.2.10")
		r.Header.Set(change.header, change.value)
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		if w.Code != 403 || w.Header().Get("Access-Control-Allow-Origin") != "" || !strings.Contains(w.Body.String(), "through this website") {
			t.Fatalf("invalid website submission (%s=%q): %d %s", change.header, change.value, w.Code, w.Body.String())
		}
	}
	if w := hostedPost(app, `{"question":"Who led MLB in OPS?"}`); !strings.Contains(w.Body.String(), `"kind":"answer"`) {
		t.Fatalf("invalid submissions consumed allowance: %s", w.Body.String())
	}
	if w := hostedPost(app, `{"question":"Who led MLB in OPS?"}`); w.Code != 429 {
		t.Fatalf("anonymous questions bypassed shared admission: %d %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("OPTIONS", "/api/questions", nil)
	websiteHeaders(r)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	if w.Code != 403 || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("public API preflight enabled")
	}
}

func TestPublicWebsiteKeepsPrivateAssetsUnavailableAndUsesSameOriginPolicy(t *testing.T) {
	svc, _ := testApp(t, PreviewModel{})
	app := NewHostedHandler(svc, HostedOptions{Mode: "production"})
	for _, path := range []string{"/mlb.db", "/config.json", "/.private/developer.log"} {
		w := httptest.NewRecorder()
		app.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatalf("private asset reachable: %s", path)
		}
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	policy := w.Header().Get("Content-Security-Policy")
	for _, directive := range []string{"script-src 'self'", "connect-src 'self'", "frame-src 'none'", "frame-ancestors 'none'", "form-action 'self'"} {
		if !strings.Contains(policy, directive) {
			t.Fatalf("public page lacks policy %s: %s", directive, policy)
		}
	}
	for _, path := range []string{"/", "/assets/app.js", "/api/client-config"} {
		w = httptest.NewRecorder()
		app.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || strings.Contains(strings.ToLower(w.Body.String()), "clerk") || strings.Contains(strings.ToLower(w.Header().Get("Content-Security-Policy")), "clerk") {
			t.Fatalf("public website still depends on retired authentication: %s", path)
		}
	}
}
