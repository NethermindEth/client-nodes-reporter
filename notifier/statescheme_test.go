package notifier

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/slack-go/slack"

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

func tableText(table *slack.TableBlock) [][]string {
	out := make([][]string, len(table.Rows))
	for i, row := range table.Rows {
		for _, cell := range row {
			text := cell.Elements[0].(*slack.RichTextSection).Elements[0].(*slack.RichTextSectionTextElement).Text
			out[i] = append(out[i], text)
		}
	}
	return out
}

func TestStateSchemeMsgHeader(t *testing.T) {
	n := &SlackNotifier{}
	want := "*Nethermind on mainnet* · Sep 29 · *2920* nodes (20.28% of EL) · flat is *62.40%* of v2"
	if got := n.buildStateSchemeMsg(fakeSchemeHistory(60)); got != want {
		t.Errorf("header = %q, want %q", got, want)
	}
}

func TestStateSchemeTable(t *testing.T) {
	want := [][]string{
		{"Scheme", "Nodes", "Share", "Change"},
		{"Pre-v2", "500", "17.12%", "-30"},
		{"v2 halfpath", "902", "30.89%", "-48"},
		{"v2 flat", "1510", "51.71%", "+51"},
		{"v2 other", "8", "0.27%", "+1"},
		{"Total", "2920", "100.00%", "-26"},
	}
	if got := tableText(buildStateSchemeTable(fakeSchemeHistory(60))); !reflect.DeepEqual(got, want) {
		t.Errorf("table = %v, want %v", got, want)
	}
}

func TestStateSchemeTableSingleReading(t *testing.T) {
	table := buildStateSchemeTable(fakeSchemeHistory(60)[:1])
	for _, row := range tableText(table) {
		if len(row) != 3 {
			t.Errorf("single reading row %v should not include a change column", row)
		}
	}
	if len(table.ColumnSettings) != 3 {
		t.Errorf("single reading has %d column settings, want 3", len(table.ColumnSettings))
	}
}

func TestStateSchemeMethodologyWarning(t *testing.T) {
	n := &SlackNotifier{}
	history := fakeSchemeHistory(60)
	render := func() string {
		raw, err := json.Marshal(n.buildStateSchemeBlocks(history, "https://example.com/chart.png"))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	if strings.Contains(render(), "methodology changed") {
		t.Error("unexpected methodology warning when methodology is unchanged")
	}
	history[len(history)-2].Methodology = "2026-08-v2"
	if msg := render(); !strings.Contains(msg, "methodology changed (`2026-08-v2` → `2026-08-v3`)") {
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
}
