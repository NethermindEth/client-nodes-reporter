package datasources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func newTestEnrScoutClient(t *testing.T, handler http.HandlerFunc, pageSize int) *EnrScoutClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := NewEnrScoutClient(&EnrScoutClientOptions{
		BaseURL:           server.URL,
		MaxRetries:        2,
		InitialRetryDelay: 0,
		PageSize:          pageSize,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestEnrScoutListNodesPaginatesAndDeduplicates(t *testing.T) {
	// 5 unique nodes; node "c" appears on two pages as if it shifted mid-crawl.
	pages := map[string][]EnrScoutNode{
		"0": {{ID: "a"}, {ID: "b"}},
		"2": {{ID: "c"}, {ID: "d"}},
		"4": {{ID: "c"}, {ID: "e"}},
		"6": {{ID: "f"}},
	}

	client := newTestEnrScoutClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/nodes" || q.Get("network") != "mainnet" || q.Get("layer") != "el" || q.Get("client") != "Nethermind" || q.Get("limit") != "2" {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		nodes := pages[q.Get("offset")]
		json.NewEncoder(w).Encode(enrScoutNodesPage{Total: 7, Count: int64(len(nodes)), Nodes: nodes})
	}, 2)

	nodes, err := client.ListNodes(context.Background(), "mainnet", EnrScoutLayerExecution, "Nethermind")
	if err != nil {
		t.Fatal(err)
	}

	var ids []string
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	if got := strings.Join(ids, ","); got != "a,b,c,d,e,f" {
		t.Errorf("ids = %s, want a,b,c,d,e,f", got)
	}
}

func TestEnrScoutClientErrorSurfacesWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	client := newTestEnrScoutClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid layer parameter"}`)
	}, 0)

	_, err := client.ListNodes(context.Background(), "mainnet", "execution", "Nethermind")
	if err == nil || !strings.Contains(err.Error(), "invalid layer parameter") {
		t.Fatalf("err = %v, want invalid layer parameter", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestEnrScoutServerErrorIsRetried(t *testing.T) {
	var calls atomic.Int32
	client := newTestEnrScoutClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, `{"methodology_version":"2026-08-v3","run_id":"abc","generated_at":"2026-09-25T15:43:06Z"}`)
	}, 0)

	meta, err := client.Meta(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if meta.MethodologyVersion != "2026-08-v3" {
		t.Errorf("MethodologyVersion = %q", meta.MethodologyVersion)
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", calls.Load())
	}
}

func TestEnrScoutServerErrorGivesUpAfterMaxRetries(t *testing.T) {
	var calls atomic.Int32
	client := newTestEnrScoutClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "boom", http.StatusServiceUnavailable)
	}, 0)

	if _, err := client.Stats(context.Background(), "mainnet"); err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", calls.Load())
	}
}
