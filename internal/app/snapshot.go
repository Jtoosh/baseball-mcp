package app

import (
	"os"
	"path/filepath"

	snapshot "github.com/jtoosh/baseball-mcp/data"
)

// The embedded snapshot is never served through the asset handler.
func OpenHostedSnapshot() (*Database, func(), error) {
	dir, err := os.MkdirTemp("", "baseball-snapshot-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	source := filepath.Join(dir, "source.csv")
	if err = os.WriteFile(source, snapshot.SnapshotCSV, 0600); err != nil {
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
