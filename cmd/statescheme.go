package cmd

import (
	"fmt"
	"log/slog"

	"client-nodes-reporter/configs"
	"client-nodes-reporter/database"
	"client-nodes-reporter/datasources"
	"client-nodes-reporter/notifier"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const stateSchemeHistorySize = 35

type stateSchemeCmdFlags struct {
	NotionDB string
	Network  string
}

func newStateSchemeCmd(common *RootCmdFlags) *cobra.Command {
	flags := new(stateSchemeCmdFlags)

	cmd := &cobra.Command{
		Use:   "nethermind-state-scheme",
		Short: "Report Nethermind v2 state scheme adoption (halfpath vs flat) from enrscout",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if flags.NotionDB == "" {
				flags.NotionDB = viper.GetString("state_scheme_notion_db")
				if flags.NotionDB == "" {
					return fmt.Errorf("state scheme notion db id is required")
				}
			}

			if flags.Network == "" {
				return fmt.Errorf("network is required")
			}

			return common.validateCommon()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			logger := ctx.Value(configs.ContextKeyLogger).(*slog.Logger)

			db, err := database.NewNotionDB(database.NotionDBOptions{
				DatabaseID: flags.NotionDB,
				Token:      common.NotionToken,
			})
			if err != nil {
				return fmt.Errorf("failed to create notion db: %w", err)
			}

			slackNotifier, err := notifier.NewSlackNotifier(notifier.SlackNotifierOptions{
				Token:   common.SlackAppToken,
				Channel: common.SlackChannel,
			})
			if err != nil {
				return fmt.Errorf("failed to create slack notifier: %w", err)
			}

			if !common.SkipUpdate {
				client, err := datasources.NewEnrScoutClient(&datasources.EnrScoutClientOptions{
					MaxRetries:        common.MaxRetries,
					InitialRetryDelay: common.InitialRetryDelay,
				})
				if err != nil {
					return fmt.Errorf("failed to create enrscout client: %w", err)
				}

				logger.Info("Fetching enrscout data", "network", flags.Network)
				meta, err := client.Meta(ctx)
				if err != nil {
					return err
				}
				stats, err := client.Stats(ctx, flags.Network)
				if err != nil {
					return err
				}
				nodes, err := client.ListNodes(ctx, flags.Network, datasources.EnrScoutLayerExecution, configs.ClientTypeNethermind.String())
				if err != nil {
					return err
				}

				snapshot := datasources.BuildStateSchemeSnapshot(flags.Network, nodes, stats, meta)
				logger.Info(
					"Resulting state scheme data",
					"network", snapshot.Network,
					"total", snapshot.Total,
					"preV2", snapshot.PreV2,
					"v2Halfpath", snapshot.V2Halfpath,
					"v2Flat", snapshot.V2Flat,
					"v2Other", snapshot.V2Other,
					"stale", snapshot.Stale,
					"networkELTotal", snapshot.NetworkELTotal,
					"methodology", snapshot.Methodology,
					"snapshotAt", snapshot.SnapshotAt,
				)

				if err := db.AddStateSchemeSnapshot(snapshot); err != nil {
					return fmt.Errorf("failed to insert state scheme data: %w", err)
				}
				logger.Info("State scheme data added successfully")
			}

			logger.Info("Getting historical state scheme data for reporting")
			history, err := db.GetLatestStateSchemeSnapshots(flags.Network, stateSchemeHistorySize)
			if err != nil {
				return fmt.Errorf("failed to get historical state scheme data: %w", err)
			}
			logger.Info("Retrieved historical state scheme data", "count", len(history))

			logger.Info("Sending state scheme report to Slack")
			if err := slackNotifier.SendStateSchemeReport(history); err != nil {
				return fmt.Errorf("failed to send state scheme report: %w", err)
			}
			logger.Info("State scheme report sent successfully")

			return nil
		},
	}

	viper.BindEnv("state_scheme_notion_db")
	cmd.Flags().StringVar(&flags.NotionDB, "notion-db", "", "notion db for state scheme snapshots. environment variable: REPORTER_STATE_SCHEME_NOTION_DB")
	cmd.Flags().StringVar(&flags.Network, "network", "mainnet", "enrscout network (mainnet, hoodi, sepolia)")

	return cmd
}
