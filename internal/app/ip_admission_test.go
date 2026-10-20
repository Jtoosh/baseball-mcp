package app

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

const fixtureIPLimitKey = "test-only-shared-ip-key-at-least-32-bytes"

func hostedIPPost(app http.Handler, ip, spoofed string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/questions", strings.NewReader(`{"question":"Who led MLB in OPS?"}`))
	websiteHeaders(req)
	req.Header.Set("Origin", "https://baseball.example")
	req.Header.Set("X-Vercel-Forwarded-For", ip)
	req.Header.Set("X-Forwarded-For", spoofed)
	req.Header.Set("X-Real-IP", spoofed)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	return w
}

func TestNetworkAllowanceSharedAcrossBrowsersAndCanonicalAddresses(t *testing.T) {
	store := operationalFixture(t)

	svc, _ := testApp(t, PreviewModel{})
	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: store})

	for i := 0; i < 20; i++ {
		w := hostedIPPost(app, "192.0.2.10", "198.51.100.1")
		if !strings.Contains(w.Body.String(), `"kind":"answer"`) {
			t.Fatalf("admission %d: %d %s", i+1, w.Code, w.Body.String())
		}
	}
	w := hostedIPPost(app, "::ffff:192.0.2.10", "198.51.100.2")
	if w.Code != 429 {
		t.Fatalf("shared network bypassed allowance: %d %s", w.Code, w.Body.String())
	}
	w = hostedIPPost(app, "192.0.2.11", "198.51.100.1")
	if !strings.Contains(w.Body.String(), `"kind":"answer"`) {
		t.Fatalf("independent network denied: %d %s", w.Code, w.Body.String())
	}
}

func TestMissingOrMalformedAuthoritativeNetworkNeverReachesProvider(t *testing.T) {
	store := operationalFixture(t)

	svc, _ := testApp(t, waitingErrorModel{"provider reached"})
	var diagnostics strings.Builder
	svc.Logger = log.New(&diagnostics, "", 0)
	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: store})

	for _, ip := range []string{"", "garbage", "192.0.2.1, 192.0.2.2", "fe80::1%en0", "0.0.0.0", "::", "224.0.0.1"} {
		diagnostics.Reset()
		w := hostedIPPost(app, ip, "203.0.113.10")
		if w.Code != 503 || !strings.Contains(w.Body.String(), "temporarily unavailable") || strings.Contains(diagnostics.String(), "interpret question") {
			t.Fatalf("bad authoritative IP reached work: %q %d %s %s", ip, w.Code, w.Body.String(), diagnostics.String())
		}
		if ip != "" && strings.Contains(diagnostics.String(), ip) {
			t.Fatalf("raw network logged: %s", diagnostics.String())
		}
	}
	req := httptest.NewRequest("POST", "/api/questions", strings.NewReader(`{"question":"Who led MLB in OPS?"}`))
	websiteHeaders(req)
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

	svc, _ := testApp(t, waitingErrorModel{"controlled provider failure"})
	svc.Config.HourlyQuestionLimit = 2
	app := NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: store})
	// Invalid JSON, empty questions, and obsolete confirmations consume no slot.
	for _, payload := range []string{`{`, `{"question":""}`, `{"question":"Who led MLB in OPS?","confirmation":"old"}`} {
		hostedPost(app, payload)
	}
	first := hostedIPPost(app, "192.0.2.10", "")
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
	resumed := NewHostedHandler(resumedService, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: restarted})
	if w := hostedIPPost(resumed, "192.0.2.10", ""); !strings.Contains(w.Body.String(), `"kind":"answer"`) {
		t.Fatalf("second admission: %s", w.Body.String())
	}
	w := hostedIPPost(resumed, "192.0.2.10", "")
	if w.Code != 429 || w.Header().Get("Retry-After") != "3540" || !strings.Contains(w.Body.String(), "2 questions") {
		t.Fatalf("failed admission not durable: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	now = now.Add(59 * time.Minute)
	if w := hostedIPPost(resumed, "192.0.2.10", ""); !strings.Contains(w.Body.String(), `"kind":"answer"`) {
		t.Fatalf("exact rolling boundary did not recover: %s", w.Body.String())
	}
	w = hostedIPPost(resumed, "192.0.2.10", "")
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("newer admission did not remain counted: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
}

func TestExistingOperationalSchemaUpgradesWithoutUsingLegacyUserLimits(t *testing.T) {
	admin := operationalFixture(t)
	schema := "upgrade_" + randomToken()[:24]
	if _, err := admin.pool.Exec(context.Background(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	dsn, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	parameters := dsn.Query()
	parameters.Set("search_path", schema)
	dsn.RawQuery = parameters.Encode()
	fixture, err := pgxpool.New(context.Background(), dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()
	// Establish a previous-release schema, including an active legacy user lease.
	_, err = fixture.Exec(context.Background(), `CREATE TABLE baseball_state_version(version integer PRIMARY KEY);
 INSERT INTO baseball_state_version VALUES(1);
 CREATE TABLE baseball_users(namespace text NOT NULL,user_id text NOT NULL,lease text NOT NULL,expires timestamptz NOT NULL,PRIMARY KEY(namespace,user_id));
 INSERT INTO baseball_users VALUES('upgraded','user_invited','legacy-active','2099-01-01');
 CREATE TABLE baseball_admissions(namespace text NOT NULL,id text NOT NULL,user_id text NOT NULL,created timestamptz NOT NULL,PRIMARY KEY(namespace,id));
 INSERT INTO baseball_admissions VALUES('upgraded','old-admission','user_invited',now());`)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := OpenOperationalStore(context.Background(), dsn.String(), "upgraded")
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()

	build := func(store *OperationalStore) http.Handler {
		svc, _ := testApp(t, PreviewModel{})
		svc.Config.HourlyQuestionLimit = 1
		return NewHostedHandler(svc, HostedOptions{IPLimitKey: fixtureIPLimitKey, Mode: "preview", PublicOrigin: "https://baseball.example", State: store})
	}
	if w := hostedIPPost(build(upgraded), "192.0.2.10", ""); !strings.Contains(w.Body.String(), `"kind":"answer"`) {
		t.Fatalf("legacy user state blocked new network admission: %d %s", w.Code, w.Body.String())
	}
	reopened, err := OpenOperationalStore(context.Background(), dsn.String(), "upgraded")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if w := hostedIPPost(build(reopened), "192.0.2.10", ""); w.Code != 429 {
		t.Fatalf("version 2 reopen lost admission: %d %s", w.Code, w.Body.String())
	}
}
