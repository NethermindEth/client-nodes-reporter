package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"
	_ "time/tzdata"

	"client-nodes-reporter/configs"
	"client-nodes-reporter/database"
	"client-nodes-reporter/datasources"

	"github.com/spf13/cobra"
)

const (
	importTableClientData  = "client-data"
	importTableStateScheme = "state-scheme"
)

// notionImport holds the rows parsed from one CSV file.
type notionImport struct {
	path        string
	table       string
	clientData  []datasources.ClientData
	stateScheme []datasources.StateSchemeSnapshot
	first, last time.Time
}

func (i *notionImport) rows() int {
	return len(i.clientData) + len(i.stateScheme)
}

func (i *notionImport) observe(t time.Time) {
	if i.first.IsZero() || t.Before(i.first) {
		i.first = t
	}
	if t.After(i.last) {
		i.last = t
	}
}

func readNotionImport(path, table string, stdin io.Reader, loc *time.Location) (*notionImport, error) {
	r := stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}

	imp := &notionImport{path: path, table: table}
	var err error
	switch table {
	case importTableClientData:
		imp.clientData, err = database.ReadNotionClientDataCSV(r, loc)
		for _, row := range imp.clientData {
			imp.observe(row.CreatedAt)
		}
	case importTableStateScheme:
		imp.stateScheme, err = database.ReadNotionStateSchemeCSV(r, loc)
		for _, row := range imp.stateScheme {
			imp.observe(row.CreatedAt)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return imp, nil
}

func (i *notionImport) store(ctx context.Context, db *database.DB) (int, error) {
	if i.table == importTableClientData {
		return db.ImportClientData(ctx, i.clientData)
	}
	return db.ImportStateSchemeSnapshots(ctx, i.stateScheme)
}

func newImportNotionCSVCmd(common *RootCmdFlags) *cobra.Command {
	var (
		table, timezone string
		dryRun          bool
	)

	cmd := &cobra.Command{
		Use:   "import-notion-csv FILE...",
		Short: "Import rows from a Notion database CSV export into the SQLite database",
		Long: `Import rows from a Notion database CSV export ("Export" -> "Markdown & CSV")
into the SQLite database. Use "-" as FILE to read from stdin.

Every file is parsed before anything is written, so a bad file imports nothing.
Rows already stored (same key and created time) are skipped, so the import can
be re-run safely. --dry-run only parses and reports what would be imported.`,
		Args: cobra.MinimumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if table != importTableClientData && table != importTableStateScheme {
				return fmt.Errorf("--table must be %q or %q", importTableClientData, importTableStateScheme)
			}
			if dryRun {
				return nil
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

			imports := make([]*notionImport, 0, len(args))
			for _, path := range args {
				imp, err := readNotionImport(path, table, cmd.InOrStdin(), loc)
				if err != nil {
					return err
				}
				logger.Info("Parsed Notion CSV", "file", path, "table", table, "rows", imp.rows(),
					"firstCreatedAt", imp.first.UTC(), "lastCreatedAt", imp.last.UTC())
				imports = append(imports, imp)
			}
			if dryRun {
				logger.Info("Dry run: nothing written")
				return nil
			}

			db, err := database.Open(ctx, common.DBPath)
			if err != nil {
				return err
			}
			defer db.Close()

			for _, imp := range imports {
				inserted, err := imp.store(ctx, db)
				if err != nil {
					return fmt.Errorf("%s: %w", imp.path, err)
				}
				logger.Info("Imported Notion CSV", "file", imp.path, "table", table,
					"read", imp.rows(), "inserted", inserted, "skipped", imp.rows()-inserted)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&table, "table", "", fmt.Sprintf("which Notion database the CSV comes from (%s, %s)", importTableClientData, importTableStateScheme))
	cmd.Flags().StringVar(&timezone, "timezone", "UTC", "IANA timezone of the Notion account that exported the CSV, used for dates without an explicit zone")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "parse the files and report what would be imported without writing")

	return cmd
}
