package notifier

import (
	"math"
	"strings"
	"testing"
	"time"

	"client-nodes-reporter/datasources"
)

// fakeSchemeHistory simulates a v2 rollout: pre-v2 drains from ~2,300 to ~500,
// halfpath peaks then declines as flat takes over, with a methodology change a
// third of the way through and a crawl hiccup two days before the end.
func fakeSchemeHistory(days int) []datasources.StateSchemeSnapshot {
	start := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	out := make([]datasources.StateSchemeSnapshot, days)
	for i := range out {
		f := float64(i) / float64(days-1)
		preV2 := int64(2300 - 1800*f)
		halfpath := int64(80 + 1400*math.Sin(f*math.Pi*0.8))
		flat := int64(10 + 1500*f*f)
		methodology := "2026-08-v3"
		if i < days/3 {
			methodology = "2026-08-v2"
		}
		if i == days-3 {
			preV2 -= 300
			halfpath -= 150
		}
		other := int64(5 + i%4)
		out[i] = datasources.StateSchemeSnapshot{
			Network:        "mainnet",
			Total:          preV2 + halfpath + flat + other,
			PreV2:          preV2,
			V2Halfpath:     halfpath,
			V2Flat:         flat,
			V2Other:        other,
			Stale:          120 + int64(i%7)*5,
			NetworkELTotal: 14000 + int64(400*f),
			Methodology:    methodology,
			SnapshotAt:     start.AddDate(0, 0, i),
			CreatedAt:      start.AddDate(0, 0, i),
		}
	}
	return out
}

func TestStateSchemeMsgMatchesLegacyStyle(t *testing.T) {
	n := &SlackNotifier{}
	history := fakeSchemeHistory(60)
	last, prev := history[len(history)-1], history[len(history)-2]
	msg := n.buildStateSchemeMsg(history)

	for _, want := range []string{
		"Today there are *2920* | *20.28%* Nethermind nodes on mainnet from which *500* | *17.12%* are still pre-v2 and *2420* | *82.88%* are on v2!",
		"On v2, *902* | *37.27%* are running halfpath and *1510* | *62.40%* are running flat (*8* other)!",
		"The number of all nodes is " + n.buildChangeMsg(last.Total-prev.Total),
		"pre-v2 nodes are " + n.buildChangeMsg(last.PreV2-prev.PreV2),
		"halfpath nodes are " + n.buildChangeMsg(last.V2Halfpath-prev.V2Halfpath),
		"flat nodes are " + n.buildChangeMsg(last.V2Flat-prev.V2Flat),
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "methodology changed") {
		t.Error("unexpected methodology warning when methodology is unchanged")
	}
}

func TestStateSchemeMsgSingleReading(t *testing.T) {
	n := &SlackNotifier{}
	msg := n.buildStateSchemeMsg(fakeSchemeHistory(60)[:1])
	if strings.Contains(msg, "The number of all nodes") {
		t.Errorf("single reading should not include changes:\n%s", msg)
	}
	if got := strings.Count(msg, "\n"); got != 1 {
		t.Errorf("single reading has %d line breaks, want 1:\n%s", got, msg)
	}
}

func TestStateSchemeMsgMethodologyWarning(t *testing.T) {
	n := &SlackNotifier{}
	history := fakeSchemeHistory(60)
	history[len(history)-2].Methodology = "2026-08-v2"
	if msg := n.buildStateSchemeMsg(history); !strings.Contains(msg, "methodology changed (`2026-08-v2` → `2026-08-v3`)") {
		t.Errorf("expected methodology warning:\n%s", msg)
	}
}

func TestStateSchemeMsgHandlesEmptyV2(t *testing.T) {
	n := &SlackNotifier{}
	msg := n.buildStateSchemeMsg([]datasources.StateSchemeSnapshot{{Network: "mainnet", Total: 10, PreV2: 10}})
	if strings.Contains(msg, "NaN") || strings.Contains(msg, "Inf") {
		t.Errorf("message has invalid percentages:\n%s", msg)
	}
}

func TestStateSchemeCharts(t *testing.T) {
	history := fakeSchemeHistory(60)

	counts := buildStateSchemeCountsChart(history)
	datasets := counts["data"].(map[string]any)["datasets"].([]map[string]any)
	if len(datasets) != 4 {
		t.Fatalf("counts chart has %d datasets, want 4", len(datasets))
	}
	if got := len(datasets[0]["data"].([]int64)); got != 60 {
		t.Errorf("counts chart has %d points, want 60", got)
	}

	flat := buildStateSchemeFlatShareChart(history)
	share := flat["data"].(map[string]any)["datasets"].([]map[string]any)[0]["data"].([]float64)
	if got := share[len(share)-1]; got != 62.4 {
		t.Errorf("last flat share = %v, want 62.4", got)
	}
	for _, v := range share {
		if v < 0 || v > 100 {
			t.Errorf("flat share %v out of range", v)
		}
	}
}
