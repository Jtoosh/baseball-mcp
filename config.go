package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
)

type Config struct {
	Model           string `json:"model"`
	DefaultStrategy string `json:"default_strategy"`
	ComparisonMode  bool   `json:"comparison_mode"`
	Database        string `json:"database"`
	Listen          string `json:"listen"`
	DeveloperLog    string `json:"developer_log"`
	EvaluationFile  string `json:"evaluation_file,omitempty"`
}

func LoadConfig(path string) (Config, error) {
	f, e := os.Open(path)
	if e != nil {
		return Config{}, e
	}
	defer f.Close()
	c := Config{DefaultStrategy: StrategySchema, Database: "mlb.db", Listen: "127.0.0.1:8080", DeveloperLog: ".private/developer.log"}
	dec := json.NewDecoder(f)
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
	if c.DefaultStrategy != StrategySchema {
		return c, errors.New("only schema strategy is currently supported")
	}
	if c.ComparisonMode {
		return c, errors.New("comparison mode is not yet supported")
	}
	return c, nil
}
