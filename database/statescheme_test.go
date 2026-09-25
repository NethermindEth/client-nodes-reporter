package database

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jomei/notionapi"

	"client-nodes-reporter/datasources"
)

func TestStateSchemeSnapshotRoundTrip(t *testing.T) {
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

	props := StateSchemeSnapshotToPageProperties(want)
	if title, ok := props[PropertySchemeNameKey].(notionapi.TitleProperty); !ok || title.Title[0].Text.Content != "nethermind-mainnet-2026-09-25" {
		t.Errorf("unexpected title property: %#v", props[PropertySchemeNameKey])
	}

	// Pages read back from Notion carry typed JSON with pointer properties, so
	// emulate that by adding the "type" discriminator and re-parsing.
	page := notionapi.Page{Properties: reparse(t, props, map[string]string{
		PropertySchemeNameKey:           "title",
		PropertySchemeNetworkKey:        "select",
		PropertySchemeTotalKey:          "number",
		PropertySchemePreV2Key:          "number",
		PropertySchemeV2HalfpathKey:     "number",
		PropertySchemeV2FlatKey:         "number",
		PropertySchemeV2OtherKey:        "number",
		PropertySchemeStaleKey:          "number",
		PropertySchemeNetworkELTotalKey: "number",
		PropertySchemeMethodologyKey:    "rich_text",
		PropertySchemeSnapshotAtKey:     "date",
	})}
	createdAt := want.CreatedAt
	page.Properties[PropertySchemeCreatedTimeKey] = &notionapi.CreatedTimeProperty{CreatedTime: createdAt}
	// PlainText is populated by Notion, not by the request payload.
	page.Properties[PropertySchemeMethodologyKey].(*notionapi.RichTextProperty).RichText[0].PlainText = want.Methodology

	got, err := PageToStateSchemeSnapshot(&page)
	if err != nil {
		t.Fatal(err)
	}
	if !got.SnapshotAt.Equal(want.SnapshotAt) {
		t.Errorf("SnapshotAt = %v, want %v", got.SnapshotAt, want.SnapshotAt)
	}
	got.SnapshotAt = want.SnapshotAt
	if got != want {
		t.Errorf("round trip mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestPageToStateSchemeSnapshotMissingProperty(t *testing.T) {
	page := notionapi.Page{Properties: notionapi.Properties{
		PropertySchemeNetworkKey: &notionapi.SelectProperty{Select: notionapi.Option{Name: "mainnet"}},
	}}
	if _, err := PageToStateSchemeSnapshot(&page); err == nil {
		t.Fatal("expected error for missing properties")
	}
}

func reparse(t *testing.T, props notionapi.Properties, types map[string]string) notionapi.Properties {
	t.Helper()
	raw := make(map[string]map[string]any, len(props))
	for key, prop := range props {
		b, err := json.Marshal(prop)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		m["type"] = types[key]
		raw[key] = m
	}

	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var out notionapi.Properties
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
