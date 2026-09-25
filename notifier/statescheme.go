package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/slack-go/slack"

	"client-nodes-reporter/datasources"
)

const quickChartCreateURL = "https://quickchart.io/chart/create"

// Categorical slots validated for CVD separation on a white surface, assigned in
// fixed order so each bucket keeps its color across both charts.
const (
	colorPreV2    = "#2a78d6"
	colorHalfpath = "#eb6834"
	colorFlat     = "#1baf7a"
	colorOther    = "#eda100"

	colorSurface   = "#ffffff"
	colorInk       = "#0b0b0b"
	colorInkSecond = "#52514e"
	colorInkMuted  = "#898781"
	colorGrid      = "#e1e0d9"
	colorBaseline  = "#c3c2b7"
)

type schemeSnapshot = datasources.StateSchemeSnapshot

func percent(part, whole int64) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) * 100 / float64(whole)
}

// buildStateSchemeMsg mirrors the wording and layout of SendReport's message.
func (n *SlackNotifier) buildStateSchemeMsg(snapshots []schemeSnapshot) string {
	last := snapshots[len(snapshots)-1]
	v2 := last.V2Total()

	msg := fmt.Sprintf(
		"Today there are *%d* | *%.2f%%* Nethermind nodes on %s from which *%d* | *%.2f%%* are still pre-v2 and *%d* | *%.2f%%* are on v2!",
		last.Total,
		percent(last.Total, last.NetworkELTotal),
		last.Network,
		last.PreV2,
		percent(last.PreV2, last.Total),
		v2,
		percent(v2, last.Total),
	)
	msg += "\n"
	msg += fmt.Sprintf(
		"On v2, *%d* | *%.2f%%* are running halfpath and *%d* | *%.2f%%* are running flat (*%d* other)!",
		last.V2Halfpath,
		percent(last.V2Halfpath, v2),
		last.V2Flat,
		percent(last.V2Flat, v2),
		last.V2Other,
	)

	if len(snapshots) > 1 {
		prev := snapshots[len(snapshots)-2]

		msg += "\n"
		msg += fmt.Sprintf(
			"The number of all nodes is %s, pre-v2 nodes are %s, halfpath nodes are %s and flat nodes are %s",
			n.buildChangeMsg(last.Total-prev.Total),
			n.buildChangeMsg(last.PreV2-prev.PreV2),
			n.buildChangeMsg(last.V2Halfpath-prev.V2Halfpath),
			n.buildChangeMsg(last.V2Flat-prev.V2Flat),
		)

		if prev.Methodology != last.Methodology {
			msg += "\n"
			msg += fmt.Sprintf(
				":warning: enrscout methodology changed (`%s` → `%s`), counts may not be comparable with previous reports",
				prev.Methodology,
				last.Methodology,
			)
		}
	}

	return msg
}

func (n *SlackNotifier) buildStateSchemeBlocks(snapshots []schemeSnapshot, countsChartURL, flatShareChartURL string) []slack.Block {
	network := snapshots[len(snapshots)-1].Network
	countsTitle := fmt.Sprintf("Nethermind %s nodes by state scheme", network)
	flatShareTitle := fmt.Sprintf("Nethermind %s flat share of v2 nodes", network)

	return []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, n.buildStateSchemeMsg(snapshots), false, false),
			nil,
			nil,
		),
		slack.NewImageBlock(
			countsChartURL,
			countsTitle,
			"quickchart-image-counts",
			slack.NewTextBlockObject(slack.PlainTextType, countsTitle, false, false),
		),
		slack.NewImageBlock(
			flatShareChartURL,
			flatShareTitle,
			"quickchart-image-flat-share",
			slack.NewTextBlockObject(slack.PlainTextType, flatShareTitle, false, false),
		),
	}
}

func (n *SlackNotifier) SendStateSchemeReport(snapshots []schemeSnapshot) error {
	if len(snapshots) == 0 {
		return fmt.Errorf("no state scheme data to report")
	}
	slices.SortFunc(snapshots, schemeSnapshot.Compare)

	countsChartURL, flatShareChartURL, err := BuildStateSchemeCharts(snapshots)
	if err != nil {
		return fmt.Errorf("failed to build state scheme charts: %w", err)
	}

	_, _, err = n.api.PostMessage(
		n.channel,
		slack.MsgOptionBlocks(n.buildStateSchemeBlocks(snapshots, countsChartURL, flatShareChartURL)...),
	)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// BuildStateSchemeCharts returns QuickChart short URLs for the stacked node counts
// and the flat-share trend. Two charts instead of one dual-axis chart, because the
// counts and the percentage do not share a scale.
func BuildStateSchemeCharts(snapshots []schemeSnapshot) (string, string, error) {
	countsURL, err := createQuickChart(buildStateSchemeCountsChart(snapshots), 900, 420)
	if err != nil {
		return "", "", fmt.Errorf("counts chart: %w", err)
	}
	flatShareURL, err := createQuickChart(buildStateSchemeFlatShareChart(snapshots), 900, 280)
	if err != nil {
		return "", "", fmt.Errorf("flat share chart: %w", err)
	}
	return countsURL, flatShareURL, nil
}

func stateSchemeLabels(snapshots []schemeSnapshot) []string {
	labels := make([]string, len(snapshots))
	for i, s := range snapshots {
		labels[i] = s.ObservedAt().UTC().Format("Jan 2")
	}
	return labels
}

func chartTitle(text string) map[string]any {
	return map[string]any{"display": true, "text": text, "fontColor": colorInk, "fontSize": 15, "fontStyle": "600", "padding": 12}
}

func chartXAxis(stacked bool) map[string]any {
	return map[string]any{
		"stacked":   stacked,
		"gridLines": map[string]any{"display": false, "drawBorder": true, "color": colorBaseline},
		"ticks":     map[string]any{"fontColor": colorInkMuted, "autoSkip": true, "maxTicksLimit": 12, "maxRotation": 0},
	}
}

func chartYAxis(label string, stacked bool, ticks map[string]any) map[string]any {
	ticks["fontColor"] = colorInkMuted
	return map[string]any{
		"stacked":    stacked,
		"gridLines":  map[string]any{"color": colorGrid, "zeroLineColor": colorBaseline, "drawBorder": false},
		"ticks":      ticks,
		"scaleLabel": map[string]any{"display": true, "labelString": label, "fontColor": colorInkSecond},
	}
}

func buildStateSchemeCountsChart(snapshots []schemeSnapshot) map[string]any {
	dataset := func(label, color string, get func(schemeSnapshot) int64) map[string]any {
		data := make([]int64, len(snapshots))
		for i, s := range snapshots {
			data[i] = get(s)
		}
		return map[string]any{
			"label":           label,
			"data":            data,
			"backgroundColor": color,
			"borderColor":     colorSurface,
			"borderWidth":     1,
			"barPercentage":   0.8,
			"maxBarThickness": 28,
		}
	}

	return map[string]any{
		"type": "bar",
		"data": map[string]any{
			"labels": stateSchemeLabels(snapshots),
			"datasets": []map[string]any{
				dataset("Pre-v2", colorPreV2, func(s schemeSnapshot) int64 { return s.PreV2 }),
				dataset("v2 halfpath", colorHalfpath, func(s schemeSnapshot) int64 { return s.V2Halfpath }),
				dataset("v2 flat", colorFlat, func(s schemeSnapshot) int64 { return s.V2Flat }),
				dataset("v2 other", colorOther, func(s schemeSnapshot) int64 { return s.V2Other }),
			},
		},
		"options": map[string]any{
			"title": chartTitle(fmt.Sprintf("Nethermind %s nodes by version and state scheme", snapshots[len(snapshots)-1].Network)),
			"legend": map[string]any{
				"display":  true,
				"position": "bottom",
				"labels":   map[string]any{"fontColor": colorInkSecond, "boxWidth": 12, "padding": 16},
			},
			"scales": map[string]any{
				"xAxes": []map[string]any{chartXAxis(true)},
				"yAxes": []map[string]any{chartYAxis("Nodes", true, map[string]any{"beginAtZero": true, "maxTicksLimit": 6})},
			},
		},
	}
}

func buildStateSchemeFlatShareChart(snapshots []schemeSnapshot) map[string]any {
	share := make([]float64, len(snapshots))
	for i, s := range snapshots {
		share[i] = math.Round(percent(s.V2Flat, s.V2Total())*10) / 10
	}

	return map[string]any{
		"type": "line",
		"data": map[string]any{
			"labels": stateSchemeLabels(snapshots),
			"datasets": []map[string]any{{
				"label":                "Flat % of v2",
				"data":                 share,
				"borderColor":          colorFlat,
				"backgroundColor":      colorFlat,
				"borderWidth":          2,
				"pointRadius":          4,
				"pointBorderColor":     colorSurface,
				"pointBorderWidth":     2,
				"pointBackgroundColor": colorFlat,
				"fill":                 false,
				"lineTension":          0,
			}},
		},
		"options": map[string]any{
			"title":  chartTitle(fmt.Sprintf("Flat share of v2 nodes: %.1f%%", share[len(share)-1])),
			"legend": map[string]any{"display": false},
			"scales": map[string]any{
				"xAxes": []map[string]any{chartXAxis(false)},
				"yAxes": []map[string]any{chartYAxis("% of v2 nodes", false, map[string]any{"min": 0, "max": 100, "stepSize": 25})},
			},
		},
	}
}

// createQuickChart uploads a chart and returns its short URL. The inline ?c= form
// used by BuildQuickChart exceeds Slack's 3000-char image_url limit for these charts.
func createQuickChart(chart map[string]any, width, height int) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"chart":            chart,
		"width":            width,
		"height":           height,
		"backgroundColor":  colorSurface,
		"devicePixelRatio": 2,
	})
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(quickChartCreateURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("quickchart returned HTTP %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Success bool   `json:"success"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode quickchart response: %w", err)
	}
	if !result.Success || result.URL == "" {
		return "", fmt.Errorf("quickchart did not return a chart url: %s", body)
	}

	return result.URL, nil
}
