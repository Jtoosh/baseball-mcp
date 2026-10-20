package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"

	configfiles "github.com/jtoosh/baseball-mcp/config"
)

type Config struct {
	DeveloperSkipClarification bool            `json:"developer_skip_clarification,omitempty"`
	DeveloperSQL               bool            `json:"developer_sql,omitempty"`
	PricingProfile             *PricingProfile `json:"pricing_profile,omitempty"`
	Model                      string          `json:"model"`
	DefaultStrategy            string          `json:"default_strategy"`
	ComparisonMode             bool            `json:"comparison_mode"`
	Database                   string          `json:"database"`
	Listen                     string          `json:"listen"`
	DeveloperLog               string          `json:"developer_log"`
	Environment                string          `json:"environment,omitempty"`
	ClerkIssuer                string          `json:"clerk_issuer,omitempty"`
	ClerkPublicKey             string          `json:"clerk_publishable_key,omitempty"`
	PublicOrigin               string          `json:"public_origin,omitempty"`
	operationalDSN             string
}

func LoadConfig(path string) (Config, error) {
	if os.Getenv("VERCEL_ENV") != "" && path == "config/hosted.json" {
		return decodeConfig(bytes.NewReader(configfiles.Hosted))
	}
	f, e := os.Open(path)
	if e != nil {
		return Config{}, e
	}
	defer f.Close()
	return decodeConfig(f)
}
func decodeConfig(input io.Reader) (Config, error) {
	var e error
	c := Config{DefaultStrategy: StrategySchema, Database: "mlb.db", Listen: "127.0.0.1:8080", DeveloperLog: ".private/developer.log"}
	dec := json.NewDecoder(input)
	dec.DisallowUnknownFields()
	if e = dec.Decode(&c); e != nil {
		return c, e
	}
	var tail any
	if e = dec.Decode(&tail); e != io.EOF {
		return c, errors.New("configuration must be a single object")
	}
	if c.Model == "" {
		return c, errors.New("configure a model identifier")
	}
	if c.DefaultStrategy != StrategySchema && c.DefaultStrategy != StrategyExamples {
		return c, errors.New("default_strategy must be schema or examples")
	}
	if c.Environment != "" && c.Environment != "local" && c.Environment != "preview" && c.Environment != "production" {
		return c, errors.New("environment must be local, preview or production")
	}
	return c, nil
}
