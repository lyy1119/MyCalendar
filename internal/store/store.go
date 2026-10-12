// Package store owns the SQLite persistence layer.
//
// The schema is the two tables the project specifies plus a small meta table
// holding the database format version. Migrations between major versions live
// in migrate.go.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	// Pure-Go SQLite driver: no cgo, so the service cross-compiles to any
	// target architecture the intranet deployment needs.
	_ "modernc.org/sqlite"
)

// Store is a handle on the SQLite database.
type Store struct {
	db   *sql.DB
	path string
}

// Open opens (creating if needed) the database at path and applies migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn, inMemory, err := buildDSN(path)
	if err != nil {
		return nil, err
	}

	// SQLite creates the database file itself, but never the directory that
	// holds it, so a sensible path like /var/lib/mycalendar/data.db fails on
	// first run with a bare "unable to open database file". Prepare the
	// directory here so that a missing one is not an error.
	if !inMemory {
		if err := prepareDataDir(path); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite allows a single writer at a time. A small pool keeps concurrent
	// readers useful while the busy_timeout pragma absorbs writer contention.
	if inMemory {
		// Every connection to :memory: would otherwise get its own database.
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(4)
	}
	db.SetMaxIdleConns(4)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		// Name the resolved path: on a first run the cause is almost always
		// the location, not the query.
		return nil, fmt.Errorf("open database %s: %w", describePath(path), err)
	}

	s := &Store{db: db, path: path}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// prepareDataDir makes sure the database file has somewhere to live.
//
// A missing parent directory is created; a parent that exists but is not a
// directory, or a path that names a directory, is reported clearly instead of
// being passed to SQLite to produce an opaque error.
func prepareDataDir(path string) error {
	if strings.HasSuffix(path, string(os.PathSeparator)) {
		return fmt.Errorf("database path %q names a directory; give a file path such as %s", path, filepath.Join(path, "mycalendar.db"))
	}

	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return fmt.Errorf("database path %q is an existing directory; give a file path such as %s", path, filepath.Join(path, "mycalendar.db"))
	}

	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		// The file lives in the working directory, which already exists.
		return nil
	}

	info, err := os.Stat(dir)
	switch {
	case err == nil:
		if !info.IsDir() {
			return fmt.Errorf("cannot use %s as a data directory: it exists and is not a directory", dir)
		}
		return nil
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create data directory %s: %w", dir, err)
		}
		return nil
	default:
		return fmt.Errorf("inspect data directory %s: %w", dir, err)
	}
}

// describePath renders a path for an error message, preferring the absolute
// form so the user does not have to guess the working directory.
func describePath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// buildDSN turns a filesystem path into a modernc.org/sqlite URI.
func buildDSN(path string) (dsn string, inMemory bool, err error) {
	if strings.TrimSpace(path) == "" {
		return "", false, fmt.Errorf("database path must not be empty")
	}
	if strings.ContainsAny(path, "?#") {
		return "", false, fmt.Errorf("database path must not contain '?' or '#': %q", path)
	}

	pragmas := "?_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)"

	if path == ":memory:" || strings.Contains(path, "mode=memory") {
		return "file::memory:" + pragmas, true, nil
	}
	return "file:" + path + pragmas, false, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the underlying handle for health checks and tests.
func (s *Store) DB() *sql.DB { return s.db }

// Path returns the configured database path.
func (s *Store) Path() string { return s.path }

// Location returns the database path in a form suitable for logging: the
// absolute file path, or the configured value for an in-memory database.
//
// Logging the absolute path removes the usual "where did the file go?"
// question, since MYCAL_DB may be relative to the working directory.
func (s *Store) Location() string {
	if s.path == ":memory:" || strings.Contains(s.path, "mode=memory") {
		return s.path
	}
	return describePath(s.path)
}
