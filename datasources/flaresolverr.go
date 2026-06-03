package datasources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// flareSolverrMaxTimeout is how long FlareSolverr's headless Chromium will
// wait for the target page to render before giving up. Cloudflare's interactive
// challenges can take 20s+, so 60s is the smallest value that reliably succeeds.
const flareSolverrMaxTimeout = 60 * time.Second

type flareSolverrRequest struct {
	Cmd        string `json:"cmd"`
	URL        string `json:"url"`
	MaxTimeout int    `json:"maxTimeout"`
}

type flareSolverrResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Solution struct {
		URL      string `json:"url"`
		Status   int    `json:"status"`
		Response string `json:"response"`
	} `json:"solution"`
}

// fetchViaFlareSolverr fetches targetURL through a FlareSolverr v1 endpoint
// (e.g. http://localhost:8191/v1) and returns the rendered HTML body plus the
// upstream HTTP status FlareSolverr observed when fetching the target.
func fetchViaFlareSolverr(solverURL, targetURL string) (string, int, error) {
	body, err := json.Marshal(flareSolverrRequest{
		Cmd:        "request.get",
		URL:        targetURL,
		MaxTimeout: int(flareSolverrMaxTimeout / time.Millisecond),
	})
	if err != nil {
		return "", 0, fmt.Errorf("marshal flaresolverr request: %w", err)
	}

	// Outer timeout is FlareSolverr's maxTimeout plus a 30s buffer for the
	// browser-launch + JSON round trip on top of the in-browser page wait.
	ctx, cancel := context.WithTimeout(context.Background(), flareSolverrMaxTimeout+30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, solverURL, bytes.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("build flaresolverr request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	slog.Debug("Fetching via FlareSolverr", "url", targetURL, "solver", solverURL)

	client := &http.Client{Timeout: flareSolverrMaxTimeout + 30*time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("flaresolverr request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("read flaresolverr response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("flaresolverr returned HTTP %d: %s", resp.StatusCode, truncateForError(string(raw)))
	}

	var parsed flareSolverrResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", 0, fmt.Errorf("decode flaresolverr response: %w", err)
	}
	if parsed.Status != "ok" {
		return "", 0, fmt.Errorf("flaresolverr failed: %s", parsed.Message)
	}

	slog.Debug("FlareSolverr returned",
		"upstreamStatus", parsed.Solution.Status,
		"bodyLength", len(parsed.Solution.Response))
	return parsed.Solution.Response, parsed.Solution.Status, nil
}

func truncateForError(s string) string {
	const max = 256
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
