package datasources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	EnrScoutLayerExecution = "el"
	EnrScoutLayerConsensus = "cl"

	enrScoutMaxPageSize = 1000
)

type EnrScoutClientOptions struct {
	BaseURL           string
	MaxRetries        int
	InitialRetryDelay time.Duration
	PageSize          int
}

type EnrScoutClient struct {
	config     EnrScoutClientOptions
	httpClient *http.Client
}

type EnrScoutMeta struct {
	GeneratedAt        time.Time `json:"generated_at"`
	MethodologyVersion string    `json:"methodology_version"`
	RunID              string    `json:"run_id"`
}

type EnrScoutStats struct {
	SnapshotGeneratedAt time.Time        `json:"snapshot_generated_at"`
	Execution           int64            `json:"execution"`
	ByClientEL          map[string]int64 `json:"by_client_el"`
}

type EnrScoutNode struct {
	ID            string `json:"id"`
	Client        string `json:"client"`
	ClientVersion string `json:"client_version"`
	FPStatus      string `json:"fp_status"`
}

type enrScoutNodesPage struct {
	Total int64          `json:"total"`
	Count int64          `json:"count"`
	Nodes []EnrScoutNode `json:"nodes"`
}

type enrScoutErrorBody struct {
	Error string `json:"error"`
}

// errRetryable marks failures worth retrying (transport errors, 5xx, 429).
type errRetryable struct{ err error }

func (e errRetryable) Error() string { return e.err.Error() }
func (e errRetryable) Unwrap() error { return e.err }

func NewEnrScoutClient(cfg *EnrScoutClientOptions) (*EnrScoutClient, error) {
	config := EnrScoutClientOptions{
		BaseURL:           "https://enrscout.ethnodeops.xyz/api/v1",
		MaxRetries:        3,
		InitialRetryDelay: 1 * time.Second,
		PageSize:          enrScoutMaxPageSize,
	}

	if cfg != nil {
		if cfg.BaseURL != "" {
			config.BaseURL = cfg.BaseURL
		}
		if cfg.MaxRetries >= 0 {
			config.MaxRetries = cfg.MaxRetries
		}
		if cfg.InitialRetryDelay >= 0 {
			config.InitialRetryDelay = cfg.InitialRetryDelay
		}
		if cfg.PageSize > 0 && cfg.PageSize <= enrScoutMaxPageSize {
			config.PageSize = cfg.PageSize
		}
	}

	return &EnrScoutClient{
		config:     config,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *EnrScoutClient) Meta(ctx context.Context) (EnrScoutMeta, error) {
	var meta EnrScoutMeta
	if err := c.getJSON(ctx, "/meta", nil, &meta); err != nil {
		return EnrScoutMeta{}, fmt.Errorf("failed to get enrscout meta: %w", err)
	}
	return meta, nil
}

func (c *EnrScoutClient) Stats(ctx context.Context, network string) (EnrScoutStats, error) {
	var stats EnrScoutStats
	if err := c.getJSON(ctx, "/stats", url.Values{"network": {network}}, &stats); err != nil {
		return EnrScoutStats{}, fmt.Errorf("failed to get enrscout stats: %w", err)
	}
	return stats, nil
}

// ListNodes returns every node matching the filters, following pagination and
// deduplicating by node id since the live crawl can shift nodes across pages.
func (c *EnrScoutClient) ListNodes(ctx context.Context, network, layer, client string) ([]EnrScoutNode, error) {
	seen := make(map[string]struct{})
	var nodes []EnrScoutNode

	for offset := 0; ; offset += c.config.PageSize {
		params := url.Values{
			"network": {network},
			"layer":   {layer},
			"client":  {client},
			"limit":   {strconv.Itoa(c.config.PageSize)},
			"offset":  {strconv.Itoa(offset)},
		}

		var page enrScoutNodesPage
		if err := c.getJSON(ctx, "/nodes", params, &page); err != nil {
			return nil, fmt.Errorf("failed to list enrscout nodes at offset %d: %w", offset, err)
		}
		slog.Debug("Fetched enrscout nodes page", "offset", offset, "count", len(page.Nodes), "total", page.Total)

		for _, node := range page.Nodes {
			if _, ok := seen[node.ID]; ok {
				continue
			}
			seen[node.ID] = struct{}{}
			nodes = append(nodes, node)
		}

		if len(page.Nodes) == 0 || int64(offset+c.config.PageSize) >= page.Total {
			break
		}
	}

	return nodes, nil
}

func (c *EnrScoutClient) getJSON(ctx context.Context, path string, params url.Values, out any) error {
	endpoint := c.config.BaseURL + path
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	var err error
	for attempt := 0; ; attempt++ {
		err = c.doGetJSON(ctx, endpoint, out)
		var retryable errRetryable
		if err == nil || !errors.As(err, &retryable) || attempt >= c.config.MaxRetries {
			return err
		}

		delay := time.Duration(int64(c.config.InitialRetryDelay) * (1 << uint(attempt)))
		slog.Info("Error during enrscout request. Retrying...", "url", endpoint, "error", err, "retries", attempt, "delay", delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

func (c *EnrScoutClient) doGetJSON(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errRetryable{err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return errRetryable{fmt.Errorf("read body: %w", err)}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		statusErr := fmt.Errorf("enrscout returned HTTP %d", resp.StatusCode)
		var apiErr enrScoutErrorBody
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Error != "" {
			statusErr = fmt.Errorf("enrscout returned HTTP %d: %s", resp.StatusCode, apiErr.Error)
		}
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return errRetryable{statusErr}
		}
		return statusErr
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
