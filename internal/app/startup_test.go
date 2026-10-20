package app

import (
	"encoding/base64"
	"net/url"
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
	t.Setenv("CLERK_ISSUER", "https://fixture.accounts.dev")
	t.Setenv("CLERK_PUBLISHABLE_KEY", "pk_test_"+base64.RawStdEncoding.EncodeToString([]byte("fixture.accounts.dev$")))
	t.Setenv("CLERK_SECRET_KEY", "sk_test_fixture")
	t.Setenv("DATABASE_URL", "postgresql://fixture:fixture@database.example/baseball?sslmode=require&channel_binding=require")
	c, err := hostedConfig(Config{})
	if err != nil {
		t.Fatalf("managed Neon URL prevented hosted startup: %v", err)
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
