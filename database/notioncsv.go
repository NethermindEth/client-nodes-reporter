package database

import (
	"encoding/csv"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"client-nodes-reporter/configs"
	"client-nodes-reporter/datasources"
)

// Layouts Notion uses for dates in its "Markdown & CSV" export, plus ISO forms
// for files edited by hand.
var notionTimeLayouts = []string{
	"January 2, 2006 3:04 PM",
	"January 2, 2006 15:04",
	"January 2, 2006",
	"2006/01/02 15:04",
	"2006/01/02",
	time.RFC3339Nano,
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

type csvTable struct {
	header map[string]int
	rows   [][]string
}

func readCSV(r io.Reader) (*csvTable, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("empty csv")
	}
	header := make(map[string]int, len(records[0]))
	for i, name := range records[0] {
		header[strings.TrimSpace(strings.TrimPrefix(name, "\uFEFF"))] = i
	}
	return &csvTable{header: header, rows: records[1:]}, nil
}

func (t *csvTable) require(columns ...string) error {
	var missing []string
	for _, c := range columns {
		if _, ok := t.header[c]; !ok {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("csv is missing columns: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (t *csvTable) get(row []string, column string) string {
	i, ok := t.header[column]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func (t *csvTable) int(row []string, column string) (int64, error) {
	raw := strings.ReplaceAll(t.get(row, column), ",", "")
	if raw == "" {
		return 0, fmt.Errorf("%s is empty", column)
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", column, err)
	}
	return int64(f), nil
}

func (t *csvTable) time(row []string, column string, loc *time.Location) (time.Time, error) {
	return parseNotionTime(t.get(row, column), loc)
}

// Notion names zones either by IANA name or as a fixed offset, e.g. "GMT-4" or "GMT+5:30".
var notionOffsetZone = regexp.MustCompile(`^(?:GMT|UTC)([+-])(\d{1,2})(?::?(\d{2}))?$`)

func notionZone(zone string) (*time.Location, error) {
	switch zone {
	case "UTC", "GMT":
		return time.UTC, nil
	}
	if m := notionOffsetZone.FindStringSubmatch(zone); m != nil {
		hours, _ := strconv.Atoi(m[2])
		minutes := 0
		if m[3] != "" {
			minutes, _ = strconv.Atoi(m[3])
		}
		offset := hours*3600 + minutes*60
		if m[1] == "-" {
			offset = -offset
		}
		return time.FixedZone(zone, offset), nil
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("timezone %q: %w", zone, err)
	}
	return loc, nil
}

// parseNotionTime parses a Notion export date. A trailing "(Zone)" suffix,
// which Notion adds to dates with an explicit timezone, overrides loc.
func parseNotionTime(raw string, loc *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if start := strings.LastIndex(raw, " ("); start != -1 && strings.HasSuffix(raw, ")") {
		zone := raw[start+2 : len(raw)-1]
		raw = raw[:start]
		z, err := notionZone(zone)
		if err != nil {
			return time.Time{}, err
		}
		loc = z
	}
	// Notion separates a date range with an arrow; keep the start.
	raw, _, _ = strings.Cut(raw, " → ")
	for _, layout := range notionTimeLayouts {
		if t, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised date %q", raw)
}

// ReadNotionClientDataCSV parses a CSV export of the client data Notion
// database. Dates without an explicit zone are read in loc.
func ReadNotionClientDataCSV(r io.Reader, loc *time.Location) ([]datasources.ClientData, error) {
	table, err := readCSV(r)
	if err != nil {
		return nil, err
	}
	if err := table.require("Source", "Client", "Total", "Client Total", "Total Synced", "Client Synced", "Created time"); err != nil {
		return nil, err
	}

	out := make([]datasources.ClientData, 0, len(table.rows))
	for i, row := range table.rows {
		d := datasources.ClientData{
			Source:     strings.ToLower(table.get(row, "Source")),
			ClientName: configs.ClientTypeFromString(table.get(row, "Client")),
		}
		var errs []error
		for _, f := range []struct {
			column string
			dst    *int64
		}{
			{"Total", &d.Total},
			{"Client Total", &d.ClientTotal},
			{"Total Synced", &d.TotalSynced},
			{"Client Synced", &d.ClientSynced},
		} {
			if *f.dst, err = table.int(row, f.column); err != nil {
				errs = append(errs, err)
			}
		}
		if d.CreatedAt, err = table.time(row, "Created time", loc); err != nil {
			errs = append(errs, err)
		} else if d.CreatedAt.IsZero() {
			errs = append(errs, fmt.Errorf("Created time is empty"))
		}
		if d.Source == "" || d.ClientName == configs.ClientTypeUnknown {
			errs = append(errs, fmt.Errorf("unknown source %q or client %q", table.get(row, "Source"), table.get(row, "Client")))
		}
		if len(errs) > 0 {
			return nil, fmt.Errorf("row %d: %v", i+2, errs)
		}
		out = append(out, d)
	}
	return out, nil
}

// ReadNotionStateSchemeCSV parses a CSV export of the state scheme Notion
// database. Dates without an explicit zone are read in loc.
func ReadNotionStateSchemeCSV(r io.Reader, loc *time.Location) ([]datasources.StateSchemeSnapshot, error) {
	table, err := readCSV(r)
	if err != nil {
		return nil, err
	}
	if err := table.require("Network", "Total", "Pre V2", "V2 Halfpath", "V2 Flat", "V2 Other", "Stale", "Network EL Total", "Created time"); err != nil {
		return nil, err
	}

	out := make([]datasources.StateSchemeSnapshot, 0, len(table.rows))
	for i, row := range table.rows {
		s := datasources.StateSchemeSnapshot{
			Network:     table.get(row, "Network"),
			Methodology: table.get(row, "Methodology"),
		}
		var errs []error
		for _, f := range []struct {
			column string
			dst    *int64
		}{
			{"Total", &s.Total},
			{"Pre V2", &s.PreV2},
			{"V2 Halfpath", &s.V2Halfpath},
			{"V2 Flat", &s.V2Flat},
			{"V2 Other", &s.V2Other},
			{"Stale", &s.Stale},
			{"Network EL Total", &s.NetworkELTotal},
		} {
			if *f.dst, err = table.int(row, f.column); err != nil {
				errs = append(errs, err)
			}
		}
		if s.SnapshotAt, err = table.time(row, "Snapshot At", loc); err != nil {
			errs = append(errs, err)
		}
		if s.CreatedAt, err = table.time(row, "Created time", loc); err != nil {
			errs = append(errs, err)
		} else if s.CreatedAt.IsZero() {
			errs = append(errs, fmt.Errorf("Created time is empty"))
		}
		if s.Network == "" {
			errs = append(errs, fmt.Errorf("Network is empty"))
		}
		if len(errs) > 0 {
			return nil, fmt.Errorf("row %d: %v", i+2, errs)
		}
		out = append(out, s)
	}
	return out, nil
}
