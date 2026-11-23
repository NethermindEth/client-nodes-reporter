package database

import (
	"context"
	"database/sql"
	"fmt"

	"client-nodes-reporter/datasources"
)

func (db *DB) AddStateSchemeSnapshot(ctx context.Context, snapshot datasources.StateSchemeSnapshot) error {
	_, err := db.insertStateSchemeSnapshot(ctx, db.sql, snapshot, false)
	return err
}

// ImportStateSchemeSnapshots inserts snapshots that keep their own CreatedAt,
// skipping any that are already stored. It returns how many were inserted.
func (db *DB) ImportStateSchemeSnapshots(ctx context.Context, snapshots []datasources.StateSchemeSnapshot) (int, error) {
	return importRows(ctx, db, snapshots, db.insertStateSchemeSnapshot)
}

func (db *DB) insertStateSchemeSnapshot(ctx context.Context, ex execer, s datasources.StateSchemeSnapshot, ignoreExisting bool) (bool, error) {
	verb := "INSERT"
	if ignoreExisting {
		verb = "INSERT OR IGNORE"
	}
	var snapshotAt sql.NullString
	if !s.SnapshotAt.IsZero() {
		snapshotAt = sql.NullString{String: formatTime(s.SnapshotAt), Valid: true}
	}
	return insertedOne(ex.ExecContext(ctx,
		verb+` INTO state_scheme_snapshots
			(network, total, pre_v2, v2_halfpath, v2_flat, v2_other, stale,
			 network_el_total, methodology, snapshot_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.Network, s.Total, s.PreV2, s.V2Halfpath, s.V2Flat, s.V2Other, s.Stale,
		s.NetworkELTotal, s.Methodology, snapshotAt, db.createdAt(s.CreatedAt),
	))
}

// GetLatestStateSchemeSnapshots returns up to limit snapshots for network, newest first.
func (db *DB) GetLatestStateSchemeSnapshots(ctx context.Context, network string, limit int) ([]datasources.StateSchemeSnapshot, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT network, total, pre_v2, v2_halfpath, v2_flat, v2_other, stale,
			network_el_total, methodology, snapshot_at, created_at
		FROM state_scheme_snapshots
		WHERE network = ?
		ORDER BY created_at DESC
		LIMIT ?`,
		network, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []datasources.StateSchemeSnapshot
	for rows.Next() {
		var (
			s          datasources.StateSchemeSnapshot
			snapshotAt sql.NullString
			created    string
		)
		if err := rows.Scan(&s.Network, &s.Total, &s.PreV2, &s.V2Halfpath, &s.V2Flat, &s.V2Other, &s.Stale,
			&s.NetworkELTotal, &s.Methodology, &snapshotAt, &created); err != nil {
			return nil, err
		}
		if snapshotAt.Valid {
			if s.SnapshotAt, err = parseTime(snapshotAt.String); err != nil {
				return nil, fmt.Errorf("state_scheme_snapshots snapshot_at %q: %w", snapshotAt.String, err)
			}
		}
		if s.CreatedAt, err = parseTime(created); err != nil {
			return nil, fmt.Errorf("state_scheme_snapshots created_at %q: %w", created, err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
