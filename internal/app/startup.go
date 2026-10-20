package app

import (
	"context"
	"errors"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
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
	c.ipLimitKey = os.Getenv("IP_LIMIT_KEY")
	if len(c.ipLimitKey) < 32 {
		return c, errors.New("configure a shared server-only IP_LIMIT_KEY of at least 32 bytes")
	}
	if c.HourlyQuestionLimit == 0 {
		c.HourlyQuestionLimit = 20
	}
	if value, exists := os.LookupEnv("HOURLY_QUESTION_LIMIT"); exists {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 {
			return c, errors.New("HOURLY_QUESTION_LIMIT must be a positive integer")
		}
		c.HourlyQuestionLimit = limit
	}
	if c.HourlyQuestionLimit < 1 {
		return c, errors.New("hourly_question_limit must be a positive integer")
	}
	c.PublicOrigin = os.Getenv("PUBLIC_ORIGIN")
	if c.Environment == "preview" {
		if host := os.Getenv("VERCEL_URL"); host != "" {
			c.PublicOrigin = "https://" + host
		}
	}
	u, err := url.Parse(c.PublicOrigin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return c, errors.New("configure an exact HTTPS public origin")
	}

	var connectionErr error
	c.operationalDSN, connectionErr = verifiedOperationalDSN(os.Getenv("DATABASE_URL"))
	if connectionErr != nil {
		return c, connectionErr
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
		live.hosted = true
		live.Client = openai.NewClient(option.WithAPIKey(os.Getenv("OPENAI_API_KEY")), option.WithBaseURL("https://api.openai.com/v1/"), option.WithMaxRetries(0))
		model = live
	}
	service := NewQueryService(db, model, c, logger)
	service.State = state
	return NewHostedHandler(service, HostedOptions{IPLimitKey: c.ipLimitKey, Mode: c.Environment, PublicOrigin: c.PublicOrigin, State: state}), func() { state.Close(); cleanup() }, nil
}
