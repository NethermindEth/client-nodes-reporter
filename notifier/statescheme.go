package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/slack-go/slack"

	"client-nodes-reporter/datasources"
)

const quickChartCreateURL = "https://quickchart.io/chart/create"

// Categorical slots validated for CVD separation on a white surface, assigned in
// fixed order so each bucket keeps its color across reports.
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

func signed(v int64) string {
	if v > 0 {
		return fmt.Sprintf("+%d", v)
	}
	return fmt.Sprintf("%d", v)
}

func previousSnapshot(snapshots []schemeSnapshot) *schemeSnapshot {
	if len(snapshots) < 2 {
		return nil
	}
	return &snapshots[len(snapshots)-2]
}

func (n *SlackNotifier) buildStateSchemeMsg(snapshots []schemeSnapshot) string {
	last := snapshots[len(snapshots)-1]
	return fmt.Sprintf(
		"*Nethermind on %s* · %s · *%d* nodes (%.2f%% of EL) · flat is *%.2f%%* of v2",
		last.Network,
		last.ObservedAt().UTC().Format("Jan 2"),
		last.Total,
		percent(last.Total, last.NetworkELTotal),
		percent(last.V2Flat, last.V2Total()),
	)
}

func tableCell(text string, bold bool) *slack.RichTextBlock {
	var style *slack.RichTextSectionTextStyle
	if bold {
		style = &slack.RichTextSectionTextStyle{Bold: true}
	}
	return slack.NewRichTextBlock("", slack.NewRichTextSection(slack.NewRichTextSectionTextElement(text, style)))
}

func buildStateSchemeTable(snapshots []schemeSnapshot) *slack.TableBlock {
	last := snapshots[len(snapshots)-1]
	prev := previousSnapshot(snapshots)

	header := []string{"Scheme", "Nodes", "Share"}
	settings := []slack.ColumnSetting{
		{Align: slack.ColumnAlignmentLeft},
		{Align: slack.ColumnAlignmentRight},
		{Align: slack.ColumnAlignmentRight},
	}
	if prev != nil {
		header = append(header, "Change")
		settings = append(settings, slack.ColumnSetting{Align: slack.ColumnAlignmentRight})
	}

	table := slack.NewTableBlock("state-scheme-table").WithColumnSettings(settings...)
	headerCells := make([]*slack.RichTextBlock, len(header))
	for i, h := range header {
		headerCells[i] = tableCell(h, true)
	}
	table.AddRow(headerCells...)

	row := func(name string, bold bool, get func(schemeSnapshot) int64) {
		cells := []*slack.RichTextBlock{
			tableCell(name, bold),
			tableCell(fmt.Sprintf("%d", get(last)), bold),
			tableCell(fmt.Sprintf("%.2f%%", percent(get(last), last.Total)), bold),
		}
		if prev != nil {
			cells = append(cells, tableCell(signed(get(last)-get(*prev)), bold))
		}
		table.AddRow(cells...)
	}
	row("Pre-v2", false, func(s schemeSnapshot) int64 { return s.PreV2 })
	row("v2 halfpath", false, func(s schemeSnapshot) int64 { return s.V2Halfpath })
	row("v2 flat", false, func(s schemeSnapshot) int64 { return s.V2Flat })
	row("v2 other", false, func(s schemeSnapshot) int64 { return s.V2Other })
	row("Total", true, func(s schemeSnapshot) int64 { return s.Total })

	return table
}

func (n *SlackNotifier) buildStateSchemeBlocks(snapshots []schemeSnapshot, chartURL string) []slack.Block {
	last := snapshots[len(snapshots)-1]
	title := fmt.Sprintf("Nethermind %s nodes by state scheme", last.Network)

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, n.buildStateSchemeMsg(snapshots), false, false),
			nil,
			nil,
		),
		buildStateSchemeTable(snapshots),
	}

	if prev := previousSnapshot(snapshots); prev != nil && prev.Methodology != last.Methodology {
		blocks = append(blocks, slack.NewContextBlock(
			"",
			slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf(
				":warning: enrscout methodology changed (`%s` → `%s`), counts may not be comparable with previous reports",
				prev.Methodology,
				last.Methodology,
			), false, false),
		))
	}

	return append(blocks, slack.NewImageBlock(
		chartURL,
		title,
		"quickchart-image-counts",
		slack.NewTextBlockObject(slack.PlainTextType, title, false, false),
	))
}

func (n *SlackNotifier) SendStateSchemeReport(snapshots []schemeSnapshot) error {
	if len(snapshots) == 0 {
		return fmt.Errorf("no state scheme data to report")
	}
	slices.SortFunc(snapshots, schemeSnapshot.Compare)

	chartURL, err := createQuickChart(buildStateSchemeCountsChart(snapshots), 900, 420)
	if err != nil {
		return fmt.Errorf("failed to build state scheme chart: %w", err)
	}

	_, _, err = n.api.PostMessage(
		n.channel,
		slack.MsgOptionBlocks(n.buildStateSchemeBlocks(snapshots, chartURL)...),
	)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
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
