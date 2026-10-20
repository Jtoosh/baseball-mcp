package app

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserServesOnlyApprovedLocalAssets(t *testing.T) {
	_, app := testApp(t, &scriptedModel{})
	for _, asset := range []struct{ path, contentType string }{
		{"/assets/app.js", "text/javascript"}, {"/assets/app.css", "text/css"}, {"/assets/marked.js", "text/javascript"}, {"/assets/purify.js", "text/javascript"},
	} {
		response := httptest.NewRecorder()
		app.ServeHTTP(response, httptest.NewRequest("GET", asset.path, nil))
		if response.Code != 200 || !strings.HasPrefix(response.Header().Get("Content-Type"), asset.contentType) || response.Body.Len() == 0 {
			t.Fatalf("asset unavailable: %s %d", asset.path, response.Code)
		}
		policy := response.Header().Get("Content-Security-Policy")
		if !strings.Contains(policy, "script-src 'self'") || strings.Contains(policy, "unsafe-inline") {
			t.Fatalf("unexpected browser script policy: %s", policy)
		}
	}
	for _, path := range []string{"/assets/mlb.db", "/assets/config.json", "/assets/developer.log", "/assets/../.private/developer.log", "/static/vendor/README.md"} {
		response := httptest.NewRecorder()
		app.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code == 200 {
			t.Fatalf("private asset served: %s", path)
		}
	}
}
