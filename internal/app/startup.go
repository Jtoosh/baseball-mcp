package app

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func hostedConfig(c Config) (Config, error) {
	switch os.Getenv("QUESTIONS_PAUSED") {
	case "":
	case "true":
		c.QuestionsPaused = true
	case "false":
		c.QuestionsPaused = false
	default:
		return c, errors.New("QUESTIONS_PAUSED must be true or false")
	}

	platform := os.Getenv("VERCEL_ENV")
	if os.Getenv("VERCEL") == "1" && platform == "" {
		return c, errors.New("hosted deployment environment is missing")
	}
	if platform != "" {
		wanted := "preview"
		if platform == "production" {
			wanted = "production"
		}
		if c.Environment != "" && c.Environment != wanted {
			return c, errors.New("configuration conflicts with deployment environment")
		}
		c.Environment = wanted
	}
	if c.Environment == "" {
		c.Environment = "local"
	}
	if c.Environment == "local" {
		return c, nil
	}
	if port := os.Getenv("PORT"); port != "" {
		c.Listen = ":" + port
	} else {
		c.Listen = ":8080"
	}
	c.ClerkIssuer = os.Getenv("CLERK_ISSUER")
	c.ClerkPublicKey = os.Getenv("CLERK_PUBLISHABLE_KEY")
	c.PublicOrigin = os.Getenv("PUBLIC_ORIGIN")
	if c.Environment == "preview" {
		if host := os.Getenv("VERCEL_URL"); host != "" {
			c.PublicOrigin = "https://" + host
		}
	}
	for _, value := range []string{c.ClerkIssuer, c.PublicOrigin} {
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return c, errors.New("configure HTTPS authentication issuer and public origin")
		}
	}
	prefix := "pk_test_"
	if c.Environment == "production" {
		prefix = "pk_live_"
	}
	if !strings.HasPrefix(c.ClerkPublicKey, prefix) {
		return c, errors.New("authentication key does not match environment")
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(strings.TrimPrefix(c.ClerkPublicKey, prefix), "="))
	if err != nil || strings.TrimSuffix(string(raw), "$") != strings.TrimPrefix(c.ClerkIssuer, "https://") {
		return c, errors.New("authentication key and issuer disagree")
	}
	secret := os.Getenv("CLERK_SECRET_KEY")
	expected := "sk_test_"
	if c.Environment == "production" {
		expected = "sk_live_"
	}
	var connectionErr error
	c.operationalDSN, connectionErr = verifiedOperationalDSN(os.Getenv("DATABASE_URL"))
	if connectionErr != nil {
		return c, connectionErr
	}
	if !strings.HasPrefix(secret, expected) || os.Getenv("DATABASE_URL") == "" {
		return c, errors.New("hosted authentication and state credentials required")
	}
	return c, nil
}

// Managed Neon URLs use sslmode=require. Strengthen the connection locally
// instead of asking owners to edit integration-managed credentials.
func verifiedOperationalDSN(dsn string) (string, error) {
	connection, err := url.Parse(dsn)
	if err != nil || (connection.Scheme != "postgres" && connection.Scheme != "postgresql") || connection.Hostname() == "" || connection.Fragment != "" {
		return "", errors.New("invalid hosted operational connection configuration")
	}
	query, err := url.ParseQuery(connection.RawQuery)
	if err != nil {
		return "", errors.New("invalid hosted operational connection configuration")
	}
	switch query.Get("sslmode") {
	case "", "require", "verify-ca", "verify-full":
		query.Set("sslmode", "verify-full")
	default:
		return "", errors.New("hosted operational connection requires verified TLS")
	}
	connection.RawQuery = query.Encode()
	return connection.String(), nil
}

func startHosted(c Config) (http.Handler, func(), error) {
	db, cleanup, err := OpenHostedSnapshot()
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	state, err := OpenOperationalStore(ctx, c.operationalDSN, c.Environment)
	if err != nil {
		cleanup()
		return nil, nil, errors.New("hosted operational storage unavailable")
	}
	logger := log.New(os.Stderr, "", log.LstdFlags)
	var model Model = PreviewModel{}
	if c.Environment == "production" {
		if os.Getenv("OPENAI_API_KEY") == "" {
			state.Close()
			cleanup()
			return nil, nil, errors.New("production model credential required")
		}
		live := NewOpenAIModel(c.Model)
		live.Client = openai.NewClient(option.WithAPIKey(os.Getenv("OPENAI_API_KEY")), option.WithBaseURL("https://api.openai.com/v1/"), option.WithMaxRetries(0))
		live.Budget = &SpendingGuard{State: state, Profile: c.PricingProfile}
		model = live
	}
	service := NewQueryService(db, model, c, logger)
	service.State = state
	auth := NewClerkAuth(os.Getenv("CLERK_SECRET_KEY"), c.ClerkIssuer, c.PublicOrigin, nil)
	return NewHostedHandler(service, HostedOptions{Mode: c.Environment, Auth: auth, PublishableKey: c.ClerkPublicKey, State: state}), func() { state.Close(); cleanup() }, nil
}
