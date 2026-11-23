package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type SlackToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// SlackRefreshFunc exchanges a refresh token for a new token pair. Slack
// accepts each refresh token only once.
type SlackRefreshFunc func(ctx context.Context, refreshToken string) (SlackToken, error)

// SlackAccessToken returns a rotating Slack bot token valid for at least
// minValidity, refreshing it when needed.
//
// The newest pair is kept in the database, because the refresh token used
// to get it is now spent. seedRefreshToken only starts the chain: it is used
// on the first run, or when it differs from the one that started the stored
// chain (an operator re-seeded it). The read, refresh and write run in one
// IMMEDIATE transaction, so processes sharing the file never spend the same
// refresh token twice.
func (db *DB) SlackAccessToken(ctx context.Context, seedRefreshToken string, minValidity time.Duration, refresh SlackRefreshFunc) (string, error) {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	seedHash := sha256.Sum256([]byte(seedRefreshToken))
	seed := hex.EncodeToString(seedHash[:])

	var stored SlackToken
	var storedSeed, expiresAt string
	err = tx.QueryRowContext(ctx,
		`SELECT access_token, refresh_token, expires_at, seed_sha256 FROM slack_tokens WHERE kind = 'bot'`,
	).Scan(&stored.AccessToken, &stored.RefreshToken, &expiresAt, &storedSeed)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return "", err
	default:
		if stored.ExpiresAt, err = parseTime(expiresAt); err != nil {
			return "", fmt.Errorf("slack_tokens expires_at %q: %w", expiresAt, err)
		}
	}

	refreshToken := seedRefreshToken
	if storedSeed == seed {
		if stored.ExpiresAt.Sub(db.now()) >= minValidity {
			return stored.AccessToken, nil
		}
		refreshToken = stored.RefreshToken
	}

	fresh, err := refresh(ctx, refreshToken)
	if err != nil {
		return "", fmt.Errorf("refresh slack token: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO slack_tokens (kind, access_token, refresh_token, expires_at, seed_sha256)
		VALUES ('bot', ?, ?, ?, ?)`,
		fresh.AccessToken, fresh.RefreshToken, formatTime(fresh.ExpiresAt), seed,
	); err != nil {
		return "", fmt.Errorf("save refreshed slack token (the stored refresh token is now spent; re-seed it): %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("save refreshed slack token (the stored refresh token is now spent; re-seed it): %w", err)
	}
	return fresh.AccessToken, nil
}
