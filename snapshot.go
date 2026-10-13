package main

import (
	_ "embed"
	"os"
	"path/filepath"
)

// The source is private binary content, never served through the asset handler.
//
//go:embed advanced-mlb-24-26.csv
var snapshotCSV []byte

func OpenHostedSnapshot() (*Database, func(), error) {
	dir, err := os.MkdirTemp("", "baseball-snapshot-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	source := filepath.Join(dir, "source.csv")
	if err = os.WriteFile(source, snapshotCSV, 0600); err != nil {
		cleanup()
		return nil, nil, err
	}
	path := filepath.Join(dir, "mlb.db")
	if err = ImportCSV(source, path); err != nil {
		cleanup()
		return nil, nil, err
	}
	os.Remove(source)
	db, err := OpenDatabase(path)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return db, func() { db.Close(); cleanup() }, nil
}
