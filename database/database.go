package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"time"

	_ "modernc.org/sqlite"
)

// Fixed-width UTC layout, so ORDER BY on the text column is chronological.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

// Each entry upgrades the schema by one version; append, never edit.
var migrations = []string{
	`CREATE TABLE client_data (
		id            INTEGER PRIMARY KEY,
		source        TEXT    NOT NULL,
		client        TEXT    NOT NULL,
		total         INTEGER NOT NULL,
		client_total  INTEGER NOT NULL,
		total_synced  INTEGER NOT NULL,
		client_synced INTEGER NOT NULL,
		created_at    TEXT    NOT NULL,
		UNIQUE (source, client, created_at)
	);
	CREATE TABLE state_scheme_snapshots (
		id               INTEGER PRIMARY KEY,
		network          TEXT    NOT NULL,
		total            INTEGER NOT NULL,
		pre_v2           INTEGER NOT NULL,
		v2_halfpath      INTEGER NOT NULL,
		v2_flat          INTEGER NOT NULL,
		v2_other         INTEGER NOT NULL,
		stale            INTEGER NOT NULL,
		network_el_total INTEGER NOT NULL,
		methodology      TEXT    NOT NULL,
		snapshot_at      TEXT,
		created_at       TEXT    NOT NULL,
		UNIQUE (network, created_at)
	);`,
	`CREATE TABLE slack_tokens (
		kind          TEXT PRIMARY KEY,
		access_token  TEXT NOT NULL,
		refresh_token TEXT NOT NULL,
		expires_at    TEXT NOT NULL,
		seed_sha256   TEXT NOT NULL
	);`,
}

type DB struct {
	sql *sql.DB
	now func() time.Time
}

// Open opens (creating if needed) the SQLite database at path and migrates it
// to the latest schema.
func Open(ctx context.Context, path string) (*DB, error) {
	dsn := "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + url.Values{
		"_pragma": {"busy_timeout(60000)"},
		"_txlock": {"immediate"},
	}.Encode()

	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db := &DB{sql: conn, now: time.Now}
	if err := db.migrate(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return db, nil
}

func (db *DB) Close() error {
	return db.sql.Close()
}

func (db *DB) migrate(ctx context.Context) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("database schema version %d is newer than this binary supports (%d)", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", len(migrations))); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) createdAt(t time.Time) string {
	if t.IsZero() {
		t = db.now()
	}
	return formatTime(t)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(timeLayout, s)
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// importRows inserts rows in one transaction, skipping rows that already exist.
func importRows[T any](ctx context.Context, db *DB, rows []T, insert func(context.Context, execer, T, bool) (bool, error)) (int, error) {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	inserted := 0
	for _, row := range rows {
		ok, err := insert(ctx, tx, row, true)
		if err != nil {
			return 0, err
		}
		if ok {
			inserted++
		}
	}
	return inserted, tx.Commit()
}

func insertedOne(res sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
