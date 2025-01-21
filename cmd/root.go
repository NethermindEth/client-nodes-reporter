package cmd

import (
	"context"
	"fmt"
	"log/slog"

	"client-nodes-reporter/configs"
	"client-nodes-reporter/database"
	"client-nodes-reporter/datasources"
	"client-nodes-reporter/notifier"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type RootCmdFlags struct {
	// Debug
	Debug bool

	// Logs
	LogsFormat string

	// Skip Update
	SkipUpdate bool

	// Source
	Source string
	// Client
	Client string

	// Notion DB
	NotionDB string
	// Notion Token
	NotionToken string

	// Slack App Token
	SlackAppToken string
	// Slack Channel
	SlackChannel string
}

func (f *RootCmdFlags) Validate() error {
	if f.Source == "" {
		return fmt.Errorf("source is required")
	}

	if f.Client == "" {
		return fmt.Errorf("client is required")
	}

	if f.NotionDB == "" {
		f.NotionDB = viper.GetString("notion_db")
		if f.NotionDB == "" {
			return fmt.Errorf("notion db id is required")
		}
	}

	if f.NotionToken == "" {
		f.NotionToken = viper.GetString("notion_token")
		if f.NotionToken == "" {
			return fmt.Errorf("notion token is required")
		}
	}

	if f.SlackAppToken == "" {
		f.SlackAppToken = viper.GetString("slack_app_token")
		if f.SlackAppToken == "" {
			return fmt.Errorf("slack app token is required")
		}
	}

	if f.SlackChannel == "" {
		f.SlackChannel = viper.GetString("slack_channel")
		if f.SlackChannel == "" {
			return fmt.Errorf("slack channel is required")
		}
	}

	return nil
}

func NewRootCmd() (*cobra.Command, error) {
	flags := new(RootCmdFlags)

	rootCmd := &cobra.Command{
		Use: "reporter",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Configure debug mode
			ctx = context.WithValue(ctx, configs.ContextKeyDebug, flags.Debug)

			// Configure logger
			loggerLevel := slog.LevelInfo
			if flags.Debug {
				loggerLevel = slog.LevelDebug
			}

			var handler slog.Handler
			if flags.LogsFormat == "json" {
				handler = slog.NewJSONHandler(cmd.OutOrStdout(), &slog.HandlerOptions{
					Level: loggerLevel,
				})
			} else if flags.LogsFormat == "text" {
				handler = slog.NewTextHandler(cmd.OutOrStdout(), &slog.HandlerOptions{
					Level: loggerLevel,
				})
			} else {
				return fmt.Errorf("invalid logs format: %s", flags.LogsFormat)
			}

			logger := slog.New(handler)
			slog.SetDefault(logger)
			slog.Debug("Configuring logger")
			ctx = context.WithValue(ctx, configs.ContextKeyLogger, logger)

			// Validate flags
			if err := flags.Validate(); err != nil {
				return err
			}

			// Configure source
			switch datasources.DataSourceType(flags.Source) {
			case datasources.DataSourceTypeEthernets:
				source := &datasources.EthernetsDataSource{}
				ctx = context.WithValue(ctx, configs.ContextKeySource, source)
			default:
				return fmt.Errorf("invalid source: \"%s\"", flags.Source)
			}

			// Configure database
			database, err := database.NewNotionDB(database.NotionDBOptions{
				DatabaseID: flags.NotionDB,
				Token:      flags.NotionToken,
			})
			if err != nil {
				return fmt.Errorf("failed to create notion db: %w", err)
			}
			ctx = context.WithValue(ctx, configs.ContextKeyDB, database)

			// Configure slack notifier
			slackNotifier, err := notifier.NewSlackNotifier(notifier.SlackNotifierOptions{
				Token:   flags.SlackAppToken,
				Channel: flags.SlackChannel,
			})
			if err != nil {
				return fmt.Errorf("failed to create slack notifier: %w", err)
			}
			ctx = context.WithValue(ctx, configs.ContextKeyNotifier, slackNotifier)

			// Update context
			cmd.SetContext(ctx)

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			logger := ctx.Value(configs.ContextKeyLogger).(*slog.Logger)
			database := ctx.Value(configs.ContextKeyDB).(*database.NotionDB)
			clientType := configs.ClientTypeFromString(flags.Client)
			if clientType == configs.ClientTypeUnknown {
				return fmt.Errorf("invalid client: %s", flags.Client)
			}

			// Updating data
			if !flags.SkipUpdate {
				logger.Info("Scanning client nodes")
				source := ctx.Value(configs.ContextKeySource).(datasources.DataSource)
				clientData, err := source.GetClientData(clientType)
				if err != nil {
					return err
				}

				logger.Info(
					"Resulting client data",
					"total", clientData.Total,
					"clientTotal", clientData.ClientTotal,
					"percentageOfNodes", fmt.Sprintf("%.2f%%", float64(clientData.ClientTotal)/float64(clientData.Total)*100),
					"totalSynced", clientData.TotalSynced,
					"clientSynced", clientData.ClientSynced,
					"percentageOfSynced", fmt.Sprintf("%.2f%%", float64(clientData.ClientSynced)/float64(clientData.TotalSynced)*100),
					"syncedPercentage", fmt.Sprintf("%.2f%%", float64(clientData.ClientSynced)/float64(clientData.ClientTotal)*100),
				)

				if err := database.AddClientData(clientData); err != nil {
					return fmt.Errorf("failed to insert client data: %w", err)
				}

				logger.Info("Client data added successfully")
			}

			// Reporting data
			historicalData, err := database.GetLatestData(flags.Client, 35, datasources.DataSourceType(flags.Source))
			if err != nil {
				return fmt.Errorf("failed to get historical data: %w", err)
			}

			notifier := ctx.Value(configs.ContextKeyNotifier).(*notifier.SlackNotifier)
			if err := notifier.SendReport(historicalData); err != nil {
				return fmt.Errorf("failed to send report: %w", err)
			}

			return nil
		},
	}

	viper.SetEnvPrefix("reporter")

	// Debug
	rootCmd.PersistentFlags().BoolVarP(&flags.Debug, "debug", "d", false, "enable debug logging")

	// Logs
	rootCmd.PersistentFlags().StringVarP(&flags.LogsFormat, "log-format", "f", "text", "logs format (json, text)")

	// Source
	rootCmd.PersistentFlags().StringVarP(&flags.Source, "source", "s", string(datasources.DataSourceTypeEthernets), "source of the client nodes")
	// Client
	rootCmd.PersistentFlags().StringVarP(&flags.Client, "client", "c", string(configs.ClientTypeNethermind), "client name")

	// Skip Update
	rootCmd.PersistentFlags().BoolVarP(&flags.SkipUpdate, "skip-update", "", false, "skip updating data")

	// Notion DB
	viper.BindEnv("notion_db")
	rootCmd.PersistentFlags().StringVarP(&flags.NotionDB, "notion-db", "", "", "notion db. environment variable: REPORTER_NOTION_DB")
	// Notion Token
	viper.BindEnv("notion_token")
	rootCmd.PersistentFlags().StringVarP(&flags.NotionToken, "notion-token", "", "", "notion token. environment variable: REPORTER_NOTION_TOKEN")

	// Slack App Token
	viper.BindEnv("slack_app_token")
	rootCmd.PersistentFlags().StringVarP(&flags.SlackAppToken, "slack-app-token", "", "", "slack app token. environment variable: REPORTER_SLACK_APP_TOKEN")
	// Slack Channel
	viper.BindEnv("slack_channel")
	rootCmd.PersistentFlags().StringVarP(&flags.SlackChannel, "slack-channel", "", "", "slack channel name or id. environment variable: REPORTER_SLACK_CHANNEL")

	return rootCmd, nil
}
