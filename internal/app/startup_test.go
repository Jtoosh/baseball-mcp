package app

import (
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestHostedConfigLoadsWithoutSourceFiles(t *testing.T) {
	t.Setenv("VERCEL_ENV", "preview")
	t.Chdir(t.TempDir())
	c, err := LoadConfig("config/hosted.json")
	if err != nil {
		t.Fatalf("embedded hosted configuration unavailable: %v", err)
	}
	if c.Model == "" || c.DefaultStrategy != StrategySchema || c.Database != "mlb.db" {
		t.Fatalf("hosted defaults missing: %+v", c)
	}
}

func TestHostedStartupAcceptsManagedNeonURL(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("VERCEL_ENV", "preview")
	t.Setenv("VERCEL_URL", "baseball-preview.example")
	t.Setenv("CLERK_ISSUER", "")
	t.Setenv("CLERK_PUBLISHABLE_KEY", "")
	t.Setenv("CLERK_SECRET_KEY", "")
	t.Setenv("IP_LIMIT_KEY", fixtureIPLimitKey)
	t.Setenv("HOURLY_QUESTION_LIMIT", "3")
	t.Setenv("DATABASE_URL", "postgresql://fixture:fixture@database.example/baseball?sslmode=require&channel_binding=require")
	c, err := hostedConfig(Config{})
	if err != nil {
		t.Fatalf("managed Neon URL prevented hosted startup: %v", err)
	}
	if c.HourlyQuestionLimit != 3 || c.ipLimitKey != fixtureIPLimitKey {
		t.Fatal("hosted admission configuration missing")
	}
	assertVerifiedOperationalTLS(t, c.operationalDSN)
}

func assertVerifiedOperationalTLS(t *testing.T, dsn string) {
	t.Helper()
	c, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("driver rejected normalized connection configuration")
	}
	if c.ConnConfig.TLSConfig == nil || c.ConnConfig.TLSConfig.InsecureSkipVerify || c.ConnConfig.TLSConfig.ServerName != "database.example" || len(c.ConnConfig.Fallbacks) != 0 {
		t.Fatal("hosted connection must verify certificates and hostname without an insecure fallback")
	}
}

func TestHostedOperationalTLSNormalization(t *testing.T) {
	for _, mode := range []string{"", "require", "verify-ca", "verify-full"} {
		t.Run(mode, func(t *testing.T) {
			raw := "postgresql://fixture:p%40ss@database.example/baseball?application_name=baseball&sslmode=" + mode
			dsn, err := verifiedOperationalDSN(raw)
			if err != nil {
				t.Fatal(err)
			}
			assertVerifiedOperationalTLS(t, dsn)
			u, _ := url.Parse(dsn)
			password, _ := u.User.Password()
			if password != "p@ss" || u.Path != "/baseball" || u.Query().Get("application_name") != "baseball" {
				t.Fatal("normalization changed connection credentials or options")
			}
		})
	}
	for _, raw := range []string{
		"", "https://database.example/baseball", "postgresql:///baseball",
		"postgresql://database.example/baseball?sslmode=disable",
		"postgresql://database.example/baseball?sslmode=prefer",
		"postgresql://database.example/baseball?sslmode=allow",
		"postgresql://database.example/baseball?sslmode=require&invalid=%zz",
	} {
		if _, err := verifiedOperationalDSN(raw); err == nil {
			t.Fatal("invalid or explicitly insecure configuration was accepted")
		}
	}
}

func TestHostedAllowanceConfigurationRejectsNonpositiveValues(t *testing.T) {
	for _, amount := range []string{"0", "-1"} {
		t.Run(amount, func(t *testing.T) {
			path := t.TempDir() + "/config.json"
			writeInspectionConfig(t, path, `{"model":"fixture","hourly_question_limit":`+amount+`}`)
			if _, err := LoadConfig(path); err == nil {
				t.Fatal("nonpositive configured allowance accepted")
			}
		})
	}
}

func TestQuestionPauseConfigurationIsValidatedAndDefaultsToAvailable(t *testing.T) {
	for _, setting := range []struct {
		raw    string
		paused bool
		valid  bool
	}{
		{`{"model":"controlled"}`, false, true},
		{`{"model":"controlled","questions_paused":true}`, true, true},
		{`{"model":"controlled","questions_paused":false}`, false, true},
		{`{"model":"controlled","questions_paused":"true"}`, false, false},
	} {
		c, err := decodeConfig(strings.NewReader(setting.raw))
		if (err == nil) != setting.valid {
			t.Fatalf("configuration validation: %s: %v", setting.raw, err)
		}
		if err == nil && c.QuestionsPaused != setting.paused {
			t.Fatalf("pause setting: %s: %v", setting.raw, c.QuestionsPaused)
		}
	}
	for _, setting := range []struct {
		value  string
		paused bool
		valid  bool
	}{
		{"", false, true}, {"true", true, true}, {"false", false, true}, {"yes", false, false},
	} {
		t.Run("environment_"+setting.value, func(t *testing.T) {
			t.Setenv("VERCEL_ENV", "")
			t.Setenv("VERCEL", "")
			t.Setenv("QUESTIONS_PAUSED", setting.value)
			c, err := hostedConfig(Config{})
			if (err == nil) != setting.valid {
				t.Fatalf("environment validation: %v", err)
			}
			if err == nil && c.QuestionsPaused != setting.paused {
				t.Fatalf("pause environment: %+v", c)
			}
		})
	}
}

func TestAnonymousHostedStartupRequiresOperationalConfiguration(t *testing.T) {
	for _, platform := range []string{"preview", "production"} {
		t.Run(platform, func(t *testing.T) {
			t.Setenv("VERCEL", "1")
			t.Setenv("VERCEL_ENV", platform)
			t.Setenv("VERCEL_URL", "")
			t.Setenv("PUBLIC_ORIGIN", "https://baseball.example")
			t.Setenv("IP_LIMIT_KEY", fixtureIPLimitKey)
			t.Setenv("DATABASE_URL", "postgres://fixture@database.example/baseball?sslmode=require")
			// No authentication settings are required in either environment.
			t.Setenv("CLERK_ISSUER", "")
			t.Setenv("CLERK_PUBLISHABLE_KEY", "")
			t.Setenv("CLERK_SECRET_KEY", "")
			t.Setenv("HOURLY_QUESTION_LIMIT", "20")
			c, err := hostedConfig(Config{})
			if err != nil || c.Environment != platform || c.PublicOrigin != "https://baseball.example" {
				t.Fatalf("anonymous hosted startup: %+v %v", c, err)
			}
			for _, bad := range []struct{ key, value string }{
				{"PUBLIC_ORIGIN", ""}, {"PUBLIC_ORIGIN", "https://baseball.example/"},
				{"PUBLIC_ORIGIN", "http://baseball.example"}, {"IP_LIMIT_KEY", ""},
				{"DATABASE_URL", ""}, {"HOURLY_QUESTION_LIMIT", "0"},
			} {
				t.Run(bad.key+bad.value, func(t *testing.T) {
					t.Setenv(bad.key, bad.value)
					if got, err := hostedConfig(Config{}); err == nil || got.Environment == "local" {
						t.Fatalf("hosted configuration did not fail closed: %+v %v", got, err)
					}
				})
			}
			t.Setenv("VERCEL_ENV", "")
			if _, err := hostedConfig(Config{}); err == nil {
				t.Fatal("missing platform environment selected local mode")
			}
		})
	}
}
