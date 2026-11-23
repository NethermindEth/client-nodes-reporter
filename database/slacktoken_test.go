package database

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeSlack issues token pairs and, like Slack, accepts each refresh token once.
type fakeSlack struct {
	mu    sync.Mutex
	valid map[string]bool
	calls int
	ttl   time.Duration
	now   func() time.Time
}

func newFakeSlack(seeds ...string) *fakeSlack {
	f := &fakeSlack{valid: map[string]bool{}, ttl: 12 * time.Hour, now: time.Now}
	for _, s := range seeds {
		f.valid[s] = true
	}
	return f
}

func (f *fakeSlack) refresh(_ context.Context, refreshToken string) (SlackToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.valid[refreshToken] {
		return SlackToken{}, fmt.Errorf("invalid_refresh_token %q", refreshToken)
	}
	delete(f.valid, refreshToken)
	f.calls++
	next := fmt.Sprintf("xoxe-1-%d", f.calls)
	f.valid[next] = true
	return SlackToken{
		AccessToken:  fmt.Sprintf("xoxe.xoxb-%d", f.calls),
		RefreshToken: next,
		ExpiresAt:    f.now().Add(f.ttl),
	}, nil
}

func TestSlackAccessTokenLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	db.now = func() time.Time { return now }
	slack := newFakeSlack("seed-a", "seed-b")
	slack.now = db.now

	get := func() string {
		t.Helper()
		token, err := db.SlackAccessToken(ctx, "seed-a", time.Hour, slack.refresh)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}

	if got := get(); got != "xoxe.xoxb-1" {
		t.Fatalf("first run = %q, want token from the seed", got)
	}

	now = now.Add(10 * time.Hour)
	if got := get(); got != "xoxe.xoxb-1" || slack.calls != 1 {
		t.Fatalf("token with 2h left = %q after %d refreshes, want stored token", got, slack.calls)
	}

	now = now.Add(90 * time.Minute)
	if got := get(); got != "xoxe.xoxb-2" {
		t.Fatalf("token with 30m left = %q, want a refreshed one", got)
	}

	// The env still holds the spent seed; the stored chain must be used instead.
	now = now.Add(24 * time.Hour)
	if got := get(); got != "xoxe.xoxb-3" {
		t.Fatalf("expired token = %q, want one refreshed from the stored chain", got)
	}

	token, err := db.SlackAccessToken(ctx, "seed-b", time.Hour, slack.refresh)
	if err != nil {
		t.Fatal(err)
	}
	if token != "xoxe.xoxb-4" {
		t.Fatalf("re-seeded token = %q, want one from the new seed", token)
	}
}

func TestSlackAccessTokenRefreshFailureKeepsStoredToken(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	slack := newFakeSlack("seed")
	if _, err := db.SlackAccessToken(ctx, "seed", time.Hour, slack.refresh); err != nil {
		t.Fatal(err)
	}

	failing := func(context.Context, string) (SlackToken, error) { return SlackToken{}, errors.New("boom") }
	if _, err := db.SlackAccessToken(ctx, "seed", 24*time.Hour, failing); err == nil {
		t.Fatal("expected refresh error")
	}

	token, err := db.SlackAccessToken(ctx, "seed", time.Hour, slack.refresh)
	if err != nil || token != "xoxe.xoxb-1" {
		t.Fatalf("got %q, %v; want the stored token", token, err)
	}
}

func TestSlackAccessTokenConcurrentProcesses(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reporter.db")
	slack := newFakeSlack("seed")

	const workers = 3
	var wg sync.WaitGroup
	tokens := make([]string, workers)
	errs := make([]error, workers)
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db, err := Open(ctx, path)
			if err != nil {
				errs[i] = err
				return
			}
			defer db.Close()
			tokens[i], errs[i] = db.SlackAccessToken(ctx, "seed", time.Hour, slack.refresh)
		}()
	}
	wg.Wait()

	for i := range workers {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if tokens[i] != "xoxe.xoxb-1" {
			t.Errorf("worker %d got %q, want the single refreshed token", i, tokens[i])
		}
	}
	if slack.calls != 1 {
		t.Errorf("refreshed %d times, want 1", slack.calls)
	}
}
