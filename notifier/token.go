package notifier

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/slack-go/slack"

	"client-nodes-reporter/database"
)

// SlackTokenRefresher renews a rotating bot token through oauth.v2.access.
func SlackTokenRefresher(clientID, clientSecret string, opts ...slack.OAuthOption) database.SlackRefreshFunc {
	client := &http.Client{Timeout: 30 * time.Second}
	return func(ctx context.Context, refreshToken string) (database.SlackToken, error) {
		resp, err := slack.RefreshOAuthV2TokenContext(ctx, client, clientID, clientSecret, refreshToken, opts...)
		if err != nil {
			return database.SlackToken{}, err
		}
		if resp.TokenType != "bot" || resp.AccessToken == "" || resp.RefreshToken == "" || resp.ExpiresIn <= 0 {
			return database.SlackToken{}, fmt.Errorf("unexpected oauth.v2.access response: token_type %q, expires_in %d", resp.TokenType, resp.ExpiresIn)
		}
		return database.SlackToken{
			AccessToken:  resp.AccessToken,
			RefreshToken: resp.RefreshToken,
			ExpiresAt:    time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second),
		}, nil
	}
}
