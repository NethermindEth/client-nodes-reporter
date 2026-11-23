package notifier

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/slack-go/slack"
)

func TestSlackTokenRefresher(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.URL.Path != "/oauth.v2.access" || r.Form.Get("grant_type") != "refresh_token" ||
			r.Form.Get("refresh_token") != "xoxe-1-old" || r.Form.Get("client_id") != "id" || r.Form.Get("client_secret") != "secret" {
			t.Errorf("unexpected request %s %v", r.URL.Path, r.Form)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"ok":            true,
			"token_type":    "bot",
			"access_token":  "xoxe.xoxb-new",
			"refresh_token": "xoxe-1-new",
			"expires_in":    43200,
		})
	}))
	defer srv.Close()

	before := time.Now()
	got, err := SlackTokenRefresher("id", "secret", slack.OAuthOptionAPIURL(srv.URL+"/"))(context.Background(), "xoxe-1-old")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "xoxe.xoxb-new" || got.RefreshToken != "xoxe-1-new" {
		t.Errorf("got %+v", got)
	}
	if d := got.ExpiresAt.Sub(before); d < 12*time.Hour || d > 12*time.Hour+time.Minute {
		t.Errorf("ExpiresAt %v is not ~12h ahead", got.ExpiresAt)
	}
}

func TestSlackTokenRefresherRejectsSlackError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid_refresh_token"})
	}))
	defer srv.Close()

	if _, err := SlackTokenRefresher("id", "secret", slack.OAuthOptionAPIURL(srv.URL+"/"))(context.Background(), "x"); err == nil {
		t.Fatal("expected error")
	}
}
