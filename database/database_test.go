package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"client-nodes-reporter/configs"
	"client-nodes-reporter/datasources"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "reporter.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reporter.db")
	for range 2 {
		db, err := Open(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

func TestClientDataRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

	for i := range 5 {
		for _, source := range []datasources.DataSourceType{datasources.DataSourceTypeEthernodes, datasources.DataSourceTypeEthernets} {
			if err := db.AddClientData(ctx, datasources.ClientData{
				Source:      string(source),
				ClientName:  configs.ClientTypeNethermind,
				Total:       10000 + int64(i),
				ClientTotal: 1000 + int64(i),
				CreatedAt:   base.AddDate(0, 0, i),
			}); err != nil {
				t.Fatal(err)
			}
		}
	}

	got, err := db.GetLatestData(ctx, "nethermind", 3, datasources.DataSourceTypeEthernets)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d rows, want 3", len(got))
	}
	want := datasources.ClientData{
		Source:      "ethernets",
		ClientName:  configs.ClientTypeNethermind,
		Total:       10004,
		ClientTotal: 1004,
		CreatedAt:   base.AddDate(0, 0, 4),
	}
	if got[0] != want {
		t.Errorf("newest row = %+v, want %+v", got[0], want)
	}
	if !got[2].CreatedAt.Equal(base.AddDate(0, 0, 2)) {
		t.Errorf("oldest row CreatedAt = %v", got[2].CreatedAt)
	}
}

func TestAddUsesNowWhenCreatedAtIsZero(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	now := time.Date(2026, 11, 23, 10, 0, 0, 123456789, time.UTC)
	db.now = func() time.Time { return now }

	if err := db.AddClientData(ctx, datasources.ClientData{Source: "ethernodes", ClientName: configs.ClientTypeNethermind}); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetLatestData(ctx, "nethermind", 1, datasources.DataSourceTypeEthernodes)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].CreatedAt.Equal(now) {
		t.Fatalf("got %+v, want CreatedAt %v", got, now)
	}
}

func TestStateSchemeRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	want := datasources.StateSchemeSnapshot{
		Network:        "mainnet",
		Total:          2337,
		PreV2:          1634,
		V2Halfpath:     576,
		V2Flat:         119,
		V2Other:        8,
		Stale:          140,
		NetworkELTotal: 14131,
		Methodology:    "2026-08-v3",
		SnapshotAt:     time.Date(2026, 9, 25, 15, 43, 6, 0, time.UTC),
		CreatedAt:      time.Date(2026, 9, 25, 16, 0, 0, 0, time.UTC),
	}
	noSnapshotAt := want
	noSnapshotAt.Network = "hoodi"
	noSnapshotAt.SnapshotAt = time.Time{}

	for _, s := range []datasources.StateSchemeSnapshot{want, noSnapshotAt} {
		if err := db.AddStateSchemeSnapshot(ctx, s); err != nil {
			t.Fatal(err)
		}
	}

	got, err := db.GetLatestStateSchemeSnapshots(ctx, "mainnet", 35)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v, want [%+v]", got, want)
	}

	got, err = db.GetLatestStateSchemeSnapshots(ctx, "hoodi", 35)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].SnapshotAt.IsZero() {
		t.Fatalf("got %+v, want one row without SnapshotAt", got)
	}
}

func TestImportSkipsExistingRows(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	rows := []datasources.ClientData{
		{Source: "ethernodes", ClientName: configs.ClientTypeNethermind, Total: 1, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Source: "ethernodes", ClientName: configs.ClientTypeNethermind, Total: 2, CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
	}

	for i, want := range []int{2, 0} {
		n, err := db.ImportClientData(ctx, rows)
		if err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Errorf("import #%d inserted %d, want %d", i+1, n, want)
		}
	}
}
