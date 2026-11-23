package database

import (
	"strings"
	"testing"
	"time"

	"client-nodes-reporter/configs"
	"client-nodes-reporter/datasources"
)

func TestReadNotionClientDataCSV(t *testing.T) {
	csv := "\uFEFFName,Client,Client Synced,Client Total,Created time,Source,Total,Total Synced\n" +
		`ethernodes-nethermind,nethermind,"1,500",2000,"October 6, 2026 10:00 AM",ethernodes,"14,131",9000` + "\n"

	loc, _ := time.LoadLocation("Europe/Madrid")
	got, err := ReadNotionClientDataCSV(strings.NewReader(csv), loc)
	if err != nil {
		t.Fatal(err)
	}
	want := datasources.ClientData{
		Source:       "ethernodes",
		ClientName:   configs.ClientTypeNethermind,
		Total:        14131,
		ClientTotal:  2000,
		TotalSynced:  9000,
		ClientSynced: 1500,
		CreatedAt:    time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC),
	}
	if len(got) != 1 || !got[0].CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("got %+v, want [%+v]", got, want)
	}
	got[0].CreatedAt = want.CreatedAt
	if got[0] != want {
		t.Errorf("got %+v, want %+v", got[0], want)
	}
}

func TestReadNotionClientDataCSVMissingColumn(t *testing.T) {
	if _, err := ReadNotionClientDataCSV(strings.NewReader("Name,Client\nx,nethermind\n"), time.UTC); err == nil {
		t.Fatal("expected error for missing columns")
	}
}

func TestReadNotionStateSchemeCSV(t *testing.T) {
	csv := "Name,Created time,Methodology,Network,Network EL Total,Pre V2,Snapshot At,Stale,Total,V2 Flat,V2 Halfpath,V2 Other\n" +
		`nethermind-mainnet-2026-09-25,"September 25, 2026 4:00 PM",2026-08-v3,mainnet,14131,1634,"September 25, 2026 3:43 PM (UTC)",140,2337,119,576,8` + "\n" +
		`nethermind-mainnet-2026-09-26,"September 26, 2026 4:00 PM",,mainnet,14131,1634,,140,2337,119,576,8` + "\n"

	got, err := ReadNotionStateSchemeCSV(strings.NewReader(csv), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
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
		SnapshotAt:     time.Date(2026, 9, 25, 15, 43, 0, 0, time.UTC),
		CreatedAt:      time.Date(2026, 9, 25, 16, 0, 0, 0, time.UTC),
	}
	if got[0] != want {
		t.Errorf("got %+v, want %+v", got[0], want)
	}
	if !got[1].SnapshotAt.IsZero() || got[1].Methodology != "" {
		t.Errorf("second row should have no snapshot time or methodology: %+v", got[1])
	}
}

func TestParseNotionTime(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
	}{
		{"", time.Time{}},
		{"November 3, 2026 9:05 PM", time.Date(2026, 11, 3, 21, 5, 0, 0, time.UTC)},
		{"November 3, 2026 21:05", time.Date(2026, 11, 3, 21, 5, 0, 0, time.UTC)},
		{"November 3, 2026", time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)},
		{"November 3, 2026 9:05 PM (America/New_York)", time.Date(2026, 11, 4, 2, 5, 0, 0, time.UTC)},
		{"November 3, 2026 9:05 PM → November 4, 2026 9:05 PM", time.Date(2026, 11, 3, 21, 5, 0, 0, time.UTC)},
		{"November 3, 2026 9:05 PM (GMT-4)", time.Date(2026, 11, 4, 1, 5, 0, 0, time.UTC)},
		{"November 3, 2026 9:05 PM (UTC+5:30)", time.Date(2026, 11, 3, 15, 35, 0, 0, time.UTC)},
		{"2026-11-03T21:05:00Z", time.Date(2026, 11, 3, 21, 5, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		got, err := parseNotionTime(c.in, time.UTC)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("%q = %v, want %v", c.in, got, c.want)
		}
	}
	if _, err := parseNotionTime("yesterday", time.UTC); err == nil {
		t.Error("expected error for unparseable date")
	}
}
