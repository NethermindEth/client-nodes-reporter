package notifier

import (
	"client-nodes-reporter/datasources"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

type QuickChartDataset struct {
	Label string   `json:"label"`
	Data  []string `json:"data"`
}

type QuickChartData struct {
	Labels   []string            `json:"labels"`
	Datasets []QuickChartDataset `json:"datasets"`
}

type QuickChartLegend struct {
	Display  bool   `json:"display"`
	Position string `json:"position"`
	Align    string `json:"align"`
}

type QuickChartOptions struct {
	Legend QuickChartLegend `json:"legend"`
}

type QuickChart struct {
	Type    string            `json:"type"`
	Data    QuickChartData    `json:"data"`
	Options QuickChartOptions `json:"options"`
}

func BuildQuickChart(
	source string,
	data []datasources.ClientData,
) (string, error) {
	allNodes := make([]string, len(data))
	syncedNodes := make([]string, len(data))
	dates := make([]string, len(data))

	for i, d := range data {
		allNodes[i] = strconv.FormatInt(d.ClientTotal, 10)
		syncedNodes[i] = strconv.FormatInt(d.ClientSynced, 10)
		dates[i] = d.CreatedAt.Format("2006-01-02")
	}

	quickChart := QuickChart{
		Type: "line",
		Data: QuickChartData{
			Labels: dates,
			Datasets: []QuickChartDataset{
				{
					Label: fmt.Sprintf("All Nodes (%s)", source),
					Data:  allNodes,
				},
				{
					Label: fmt.Sprintf("Synced Nodes (%s)", source),
					Data:  syncedNodes,
				},
			},
		},
		Options: QuickChartOptions{
			Legend: QuickChartLegend{
				Display:  true,
				Position: "right",
				Align:    "start",
			},
		},
	}

	json, err := json.Marshal(quickChart)
	if err != nil {
		return "", err
	}

	query := url.QueryEscape(string(json))

	return fmt.Sprintf("https://quickchart.io/chart?c=%s", query), nil
}
