package notifier

import (
	"client-nodes-reporter/datasources"
	"fmt"
	"log/slog"
	"slices"

	"github.com/slack-go/slack"
)

type NotifierReport struct {
	SourceName string
	ClientData []datasources.ClientData
}

type SlackNotifierOptions struct {
	Token   string
	Channel string
}

type SlackNotifier struct {
	channel string
	api     *slack.Client
}

func NewSlackNotifier(options SlackNotifierOptions) (*SlackNotifier, error) {
	client := slack.New(options.Token)

	return &SlackNotifier{
		options.Channel,
		client,
	}, nil
}

func (n *SlackNotifier) buildChangeMsg(change int64) string {
	if change < 0 {
		return fmt.Sprintf("*shrinking* ( *%d* :fire_extinguisher:)", change)
	} else if change > 0 {
		return fmt.Sprintf("*growing* ( *+%d* :muscle:)", change)
	} else {
		return "*not moving* ( *0* :no_mouth:)"
	}
}

func (n *SlackNotifier) SendReport(report NotifierReport) error {
	slices.SortFunc(report.ClientData, datasources.ClientData.Compare)
	lastUpdate := report.ClientData[len(report.ClientData)-1]
	client := lastUpdate.ClientName.String()

	reportMsg := fmt.Sprintf(
		"Today there are *%d* | *%.2f%%* %s nodes from which *%d* | *%.2f%%* are synced(*%.2f%%*)!",
		lastUpdate.ClientTotal,
		(float64(lastUpdate.ClientTotal)*100)/float64(lastUpdate.Total),
		client,
		lastUpdate.ClientSynced,
		(float64(lastUpdate.ClientSynced)*100)/float64(lastUpdate.TotalSynced),
		(float64(lastUpdate.ClientSynced)*100)/float64(lastUpdate.ClientTotal),
	)

	if len(report.ClientData) > 1 {
		previousUpdate := report.ClientData[len(report.ClientData)-2]

		totalChange := lastUpdate.ClientTotal - previousUpdate.ClientTotal
		SyncedChange := lastUpdate.ClientSynced - previousUpdate.ClientSynced

		reportMsg += "\n"
		reportMsg += fmt.Sprintf(
			"The number of all nodes is %s and synced nodes are %s",
			n.buildChangeMsg(totalChange),
			n.buildChangeMsg(SyncedChange),
		)
	}

	quickChart, err := BuildQuickChart(report.SourceName, report.ClientData)
	if err != nil {
		return fmt.Errorf("failed to build quick chart: %w", err)
	}

	result, _, err := n.api.PostMessage(
		n.channel,
		slack.MsgOptionBlocks(
			slack.NewSectionBlock(
				slack.NewTextBlockObject(
					slack.MarkdownType,
					reportMsg,
					false,
					false,
				),
				nil,
				nil,
			),
			slack.NewImageBlock(
				quickChart,
				fmt.Sprintf("%s client nodes", client),
				"quickchart-image",
				slack.NewTextBlockObject(
					slack.PlainTextType,
					fmt.Sprintf("%s client nodes", client),
					false,
					false,
				),
			),
		),
	)
	slog.Debug("Slack message sent", "result", result)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}
