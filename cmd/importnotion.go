package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"time"
	_ "time/tzdata"

	"client-nodes-reporter/configs"
	"client-nodes-reporter/database"

	"github.com/spf13/cobra"
)

const (
	importTableClientData  = "client-data"
	importTableStateScheme = "state-scheme"
)

func newImportNotionCSVCmd(common *RootCmdFlags) *cobra.Command {
	var table, timezone string

	cmd := &cobra.Command{
		Use:   "import-notion-csv FILE...",
		Short: "Import rows from a Notion database CSV export into the SQLite database",
		Long: `Import rows from a Notion database CSV export ("Export" -> "Markdown & CSV")
into the SQLite database. Rows already stored (same key and created time) are
skipped, so the import can be re-run safely.`,
		Args: cobra.MinimumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if table != importTableClientData && table != importTableStateScheme {
				return fmt.Errorf("--table must be %q or %q", importTableClientData, importTableStateScheme)
			}
			return common.validateDB()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			logger := ctx.Value(configs.ContextKeyLogger).(*slog.Logger)

			loc, err := time.LoadLocation(timezone)
			if err != nil {
				return fmt.Errorf("invalid --timezone: %w", err)
			}

			db, err := database.Open(ctx, common.DBPath)
			if err != nil {
				return err
			}
			defer db.Close()

			for _, path := range args {
				f, err := os.Open(path)
				if err != nil {
					return err
				}

				var read, inserted int
				switch table {
				case importTableClientData:
					rows, err := database.ReadNotionClientDataCSV(f, loc)
					f.Close()
					if err != nil {
						return fmt.Errorf("%s: %w", path, err)
					}
					read = len(rows)
					inserted, err = db.ImportClientData(ctx, rows)
					if err != nil {
						return fmt.Errorf("%s: %w", path, err)
					}
				case importTableStateScheme:
					rows, err := database.ReadNotionStateSchemeCSV(f, loc)
					f.Close()
					if err != nil {
						return fmt.Errorf("%s: %w", path, err)
					}
					read = len(rows)
					inserted, err = db.ImportStateSchemeSnapshots(ctx, rows)
					if err != nil {
						return fmt.Errorf("%s: %w", path, err)
					}
				}
				logger.Info("Imported Notion CSV", "file", path, "table", table, "read", read, "inserted", inserted, "skipped", read-inserted)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&table, "table", "", fmt.Sprintf("which Notion database the CSV comes from (%s, %s)", importTableClientData, importTableStateScheme))
	cmd.Flags().StringVar(&timezone, "timezone", "UTC", "IANA timezone of the Notion account that exported the CSV, used for dates without an explicit zone")

	return cmd
}
