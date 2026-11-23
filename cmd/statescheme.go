package cmd

import (
	"fmt"
	"log/slog"

	"client-nodes-reporter/configs"
	"client-nodes-reporter/database"
	"client-nodes-reporter/datasources"

	"github.com/spf13/cobra"
)

const stateSchemeHistorySize = 35

type stateSchemeCmdFlags struct {
	Network string
}

func newStateSchemeCmd(common *RootCmdFlags) *cobra.Command {
	flags := new(stateSchemeCmdFlags)

	cmd := &cobra.Command{
		Use:   "nethermind-state-scheme",
		Short: "Report Nethermind v2 state scheme adoption (halfpath vs flat) from enrscout",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if flags.Network == "" {
				return fmt.Errorf("network is required")
			}

			return common.validateCommon()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			logger := ctx.Value(configs.ContextKeyLogger).(*slog.Logger)

			db, err := database.Open(ctx, common.DBPath)
			if err != nil {
				return err
			}
			defer db.Close()

			slackNotifier, err := common.newSlackNotifier(ctx, db)
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

				if err := db.AddStateSchemeSnapshot(ctx, snapshot); err != nil {
					return fmt.Errorf("failed to insert state scheme data: %w", err)
				}
				logger.Info("State scheme data added successfully")
			}

			logger.Info("Getting historical state scheme data for reporting")
			history, err := db.GetLatestStateSchemeSnapshots(ctx, flags.Network, stateSchemeHistorySize)
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

	cmd.Flags().StringVar(&flags.Network, "network", "mainnet", "enrscout network (mainnet, hoodi, sepolia)")

	return cmd
}
