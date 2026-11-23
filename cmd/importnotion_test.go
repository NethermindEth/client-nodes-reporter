package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"client-nodes-reporter/database"
	"client-nodes-reporter/datasources"
)

const clientDataCSV = "Name,Client,Client Synced,Client Total,Created time,Source,Total,Total Synced\n" +
	`x,nethermind,1500,2000,"October 6, 2026 10:00 AM",ethernets,14131,9000` + "\n" +
	`x,nethermind,1510,2010,"October 7, 2026 10:00 AM",ethernets,14140,9010` + "\n"

func runImport(t *testing.T, stdin string, args ...string) error {
	t.Helper()
	root, err := NewRootCmd()
	if err != nil {
		t.Fatal(err)
	}
	root.SetArgs(append([]string{"import-notion-csv", "--log-format", "text"}, args...))
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	return root.ExecuteContext(context.Background())
}

func storedClientData(t *testing.T, dbPath string) []datasources.ClientData {
	t.Helper()
	db, err := database.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.GetLatestData(context.Background(), "nethermind", 100, datasources.DataSourceTypeEthernets)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestImportNotionCSVFromStdin(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reporter.db")
	for range 2 {
		if err := runImport(t, clientDataCSV, "--db-path", dbPath, "--table", "client-data", "-"); err != nil {
			t.Fatal(err)
		}
	}
	if got := storedClientData(t, dbPath); len(got) != 2 {
		t.Fatalf("stored %d rows, want 2", len(got))
	}
}

func TestImportNotionCSVDryRunWritesNothing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reporter.db")
	if err := runImport(t, clientDataCSV, "--db-path", dbPath, "--table", "client-data", "--dry-run", "-"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("dry run created the database: %v", err)
	}
}

func TestImportNotionCSVBadFileImportsNothing(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "reporter.db")
	bad := filepath.Join(dir, "bad.csv")
	if err := os.WriteFile(bad, []byte("Name,Client\nx,nethermind\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runImport(t, clientDataCSV, "--db-path", dbPath, "--table", "client-data", "-", bad); err == nil {
		t.Fatal("expected error for the bad file")
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("a failed import created the database: %v", err)
	}
}
