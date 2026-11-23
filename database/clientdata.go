package database

import (
	"context"
	"fmt"

	"client-nodes-reporter/configs"
	"client-nodes-reporter/datasources"
)

func (db *DB) AddClientData(ctx context.Context, data datasources.ClientData) error {
	_, err := db.insertClientData(ctx, db.sql, data, false)
	return err
}

// ImportClientData inserts rows that keep their own CreatedAt, skipping any
// that are already stored. It returns how many rows were inserted.
func (db *DB) ImportClientData(ctx context.Context, rows []datasources.ClientData) (int, error) {
	return importRows(ctx, db, rows, db.insertClientData)
}

func (db *DB) insertClientData(ctx context.Context, ex execer, data datasources.ClientData, ignoreExisting bool) (bool, error) {
	verb := "INSERT"
	if ignoreExisting {
		verb = "INSERT OR IGNORE"
	}
	return insertedOne(ex.ExecContext(ctx,
		verb+` INTO client_data
			(source, client, total, client_total, total_synced, client_synced, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
		data.Source, string(data.ClientName),
		data.Total, data.ClientTotal, data.TotalSynced, data.ClientSynced,
		db.createdAt(data.CreatedAt),
	))
}

// GetLatestData returns up to limit rows for client and source, newest first.
func (db *DB) GetLatestData(ctx context.Context, client string, limit int, source datasources.DataSourceType) ([]datasources.ClientData, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT source, client, total, client_total, total_synced, client_synced, created_at
		FROM client_data
		WHERE source = ? AND client = ?
		ORDER BY created_at DESC
		LIMIT ?`,
		string(source), client, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []datasources.ClientData
	for rows.Next() {
		var (
			d               datasources.ClientData
			client, created string
		)
		if err := rows.Scan(&d.Source, &client, &d.Total, &d.ClientTotal, &d.TotalSynced, &d.ClientSynced, &created); err != nil {
			return nil, err
		}
		d.ClientName = configs.ClientTypeFromString(client)
		if d.CreatedAt, err = parseTime(created); err != nil {
			return nil, fmt.Errorf("client_data created_at %q: %w", created, err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
