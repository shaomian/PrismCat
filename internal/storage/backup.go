package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// Snapshot writes a standalone database without changing the source schema.
func (r *SQLiteRepository) Snapshot(ctx context.Context, destination string) error {
	_, err := r.db.ExecContext(ctx, "VACUUM INTO ?", destination)
	return err
}

func (a *AsyncRepository) Snapshot(ctx context.Context, destination string) error {
	if repo, ok := a.inner.(interface {
		Snapshot(context.Context, string) error
	}); ok {
		return repo.Snapshot(ctx, destination)
	}
	return fmt.Errorf("database snapshots unavailable")
}

// OpenBackupDatabase bypasses migrations and runtime recovery. mode=rw refuses
// to create a missing source; mode=ro is used for inspecting isolated snapshots.
func OpenBackupDatabase(path string, readOnly bool) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("database must be a regular file")
	}
	mode := "rw"
	if readOnly {
		mode = "ro"
	}
	u := &url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	query := url.Values{"mode": {mode}, "_pragma": {"busy_timeout=5000"}}
	u.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func SnapshotFile(ctx context.Context, source, destination string) error {
	db, err := OpenBackupDatabase(source, false)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, "VACUUM INTO ?", destination)
	return err
}
