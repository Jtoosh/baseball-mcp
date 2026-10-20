// Package data embeds the fixed MLB batting snapshot for hosted deployments.
package data

import _ "embed"

// SnapshotCSV is the source used to build the read-only hosted SQLite database.
//
//go:embed advanced-mlb-24-26.csv
var SnapshotCSV []byte
