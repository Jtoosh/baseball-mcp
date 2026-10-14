package main

import (
	"context"
	"net"
	"strings"
)

type SQLInspection struct {
	Strategy string       `json:"strategy"`
	Queries  []SQLAttempt `json:"queries"`
}

type SQLAttempt struct {
	SQL    string `json:"sql"`
	Failed bool   `json:"failed,omitempty"`
}

type sqlInspectionKey struct{}

func (c Config) sqlInspectionEnabled() bool {
	if !c.DeveloperSQL || (c.Environment != "" && c.Environment != "local") {
		return false
	}
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func inspectPlan(ctx context.Context, strategy, sql string) {
	inspections, _ := ctx.Value(sqlInspectionKey{}).([]SQLInspection)
	if strings.TrimSpace(sql) == "" {
		return
	}
	for i := range inspections {
		if inspections[i].Strategy == strategy {
			inspections[i].Queries = append(inspections[i].Queries, SQLAttempt{SQL: sql})
			return
		}
	}
}

func inspectExecution(ctx context.Context, strategy string, failed bool) {
	inspections, _ := ctx.Value(sqlInspectionKey{}).([]SQLInspection)
	for i := range inspections {
		if inspections[i].Strategy == strategy && len(inspections[i].Queries) > 0 {
			inspections[i].Queries[len(inspections[i].Queries)-1].Failed = failed
			return
		}
	}
}
