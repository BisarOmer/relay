package storage

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/relay-ide/relay/engine/migrations"
	_ "modernc.org/sqlite"
)

type DB struct {
	SQL *sql.DB
	// ReadOnly is a separate pool opened with mode=ro and query_only so
	// user-authored queries can never write and never block the writer.
	ReadOnly *sql.DB
	Dir      string
	// Recovered is set when an unreadable database file was moved aside and
	// a fresh one was created.
	Recovered     bool
	RecoveredPath string
}

// Open opens (and migrates) relay.db under dataDir. When migrationsDir is
// empty the migrations embedded in the binary are used.
func Open(dataDir string, migrationsDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	var migr fs.FS = migrations.FS
	if migrationsDir != "" {
		migr = os.DirFS(migrationsDir)
	}
	dbPath := filepath.Join(dataDir, "relay.db")
	db, err := open(dbPath, dataDir, migr)
	if err == nil {
		return db, nil
	}
	if !isCorruption(err) {
		return nil, err
	}
	aside := fmt.Sprintf("%s.corrupt-%s", dbPath, time.Now().UTC().Format("20060102T150405Z"))
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, statErr := os.Stat(dbPath + suffix); statErr == nil {
			if renameErr := os.Rename(dbPath+suffix, aside+suffix); renameErr != nil {
				return nil, fmt.Errorf("database unreadable (%v) and could not be moved aside: %w", err, renameErr)
			}
		}
	}
	db, err2 := open(dbPath, dataDir, migr)
	if err2 != nil {
		return nil, err2
	}
	db.Recovered = true
	db.RecoveredPath = aside
	return db, nil
}

func open(dbPath, dataDir string, migr fs.FS) (*DB, error) {
	slash := filepath.ToSlash(dbPath)
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", slash)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	db := &DB{SQL: sqlDB, Dir: dataDir}
	if err := db.migrate(migr); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	roDSN := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(2000)&_pragma=query_only(1)", slash)
	ro, err := sql.Open("sqlite", roDSN)
	if err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	ro.SetMaxOpenConns(2)
	ro.SetConnMaxIdleTime(time.Minute)
	db.ReadOnly = ro
	return db, nil
}

func isCorruption(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "file is not a database") ||
		strings.Contains(msg, "database disk image is malformed")
}

func (db *DB) Close() error {
	if db.ReadOnly != nil {
		_ = db.ReadOnly.Close()
	}
	return db.SQL.Close()
}

func (db *DB) migrate(migr fs.FS) error {
	if _, err := db.SQL.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return err
	}

	entries, err := fs.ReadDir(migr, ".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("no migrations found")
	}
	sort.Strings(files)

	for _, name := range files {
		var version int
		if _, err := fmt.Sscanf(name, "%d_", &version); err != nil {
			continue
		}
		var exists int
		if err := db.SQL.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, version).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		body, err := fs.ReadFile(migr, name)
		if err != nil {
			return err
		}
		up := extractUp(string(body))
		tx, err := db.SQL.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(up); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, datetime('now'))`, version); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// SchemaVersion returns the highest applied migration version.
func (db *DB) SchemaVersion() int {
	var v sql.NullInt64
	_ = db.SQL.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	return int(v.Int64)
}

func extractUp(sqlText string) string {
	const upMarker = "-- +migrate Up"
	const downMarker = "-- +migrate Down"
	start := strings.Index(sqlText, upMarker)
	if start < 0 {
		return sqlText
	}
	start += len(upMarker)
	end := strings.Index(sqlText[start:], downMarker)
	if end < 0 {
		return strings.TrimSpace(sqlText[start:])
	}
	return strings.TrimSpace(sqlText[start : start+end])
}
