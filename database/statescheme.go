package database

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jomei/notionapi"

	"client-nodes-reporter/datasources"
)

const (
	PropertySchemeNameKey           = "Name"
	PropertySchemeNetworkKey        = "Network"
	PropertySchemeTotalKey          = "Total"
	PropertySchemePreV2Key          = "Pre V2"
	PropertySchemeV2HalfpathKey     = "V2 Halfpath"
	PropertySchemeV2FlatKey         = "V2 Flat"
	PropertySchemeV2OtherKey        = "V2 Other"
	PropertySchemeStaleKey          = "Stale"
	PropertySchemeNetworkELTotalKey = "Network EL Total"
	PropertySchemeMethodologyKey    = "Methodology"
	PropertySchemeSnapshotAtKey     = "Snapshot At"
	PropertySchemeCreatedTimeKey    = "Created time"
)

func (db *NotionDB) AddStateSchemeSnapshot(snapshot datasources.StateSchemeSnapshot) error {
	_, err := db.client.Page.Create(
		context.Background(),
		&notionapi.PageCreateRequest{
			Parent: notionapi.Parent{
				DatabaseID: notionapi.DatabaseID(db.database.ID.String()),
			},
			Properties: StateSchemeSnapshotToPageProperties(snapshot),
		},
	)
	return err
}

func (db *NotionDB) GetLatestStateSchemeSnapshots(network string, pageSize int) ([]datasources.StateSchemeSnapshot, error) {
	slog.Debug("Querying Notion state scheme database", "network", network, "pageSize", pageSize)

	response, err := db.client.Database.Query(
		context.Background(),
		notionapi.DatabaseID(db.database.ID),
		&notionapi.DatabaseQueryRequest{
			Filter: &notionapi.PropertyFilter{
				Property: PropertySchemeNetworkKey,
				Select: &notionapi.SelectFilterCondition{
					Equals: network,
				},
			},
			Sorts: []notionapi.SortObject{
				{
					Timestamp: notionapi.TimestampCreated,
					Direction: notionapi.SortOrderDESC,
				},
			},
			PageSize: pageSize,
		},
	)
	if err != nil {
		return nil, err
	}

	snapshots := make([]datasources.StateSchemeSnapshot, 0, len(response.Results))
	for _, page := range response.Results {
		snapshot, err := PageToStateSchemeSnapshot(&page)
		if err != nil {
			return nil, fmt.Errorf("page %s: %w", page.ID, err)
		}
		snapshots = append(snapshots, snapshot)
	}

	return snapshots, nil
}

func StateSchemeSnapshotToPageProperties(snapshot datasources.StateSchemeSnapshot) notionapi.Properties {
	return notionapi.Properties{
		PropertySchemeNameKey:           BuildTitleProperty(fmt.Sprintf("nethermind-%s-%s", snapshot.Network, snapshot.CreatedAt.UTC().Format("2006-01-02"))),
		PropertySchemeNetworkKey:        BuildSelectProperty(snapshot.Network),
		PropertySchemeTotalKey:          BuildNumberProperty(float64(snapshot.Total)),
		PropertySchemePreV2Key:          BuildNumberProperty(float64(snapshot.PreV2)),
		PropertySchemeV2HalfpathKey:     BuildNumberProperty(float64(snapshot.V2Halfpath)),
		PropertySchemeV2FlatKey:         BuildNumberProperty(float64(snapshot.V2Flat)),
		PropertySchemeV2OtherKey:        BuildNumberProperty(float64(snapshot.V2Other)),
		PropertySchemeStaleKey:          BuildNumberProperty(float64(snapshot.Stale)),
		PropertySchemeNetworkELTotalKey: BuildNumberProperty(float64(snapshot.NetworkELTotal)),
		PropertySchemeMethodologyKey:    BuildRichTextProperty(snapshot.Methodology),
		PropertySchemeSnapshotAtKey:     BuildDateProperty(snapshot.SnapshotAt),
	}
}

func PageToStateSchemeSnapshot(page *notionapi.Page) (datasources.StateSchemeSnapshot, error) {
	var snapshot datasources.StateSchemeSnapshot
	var ok bool

	if snapshot.Network, ok = GetSelectValue(page.Properties[PropertySchemeNetworkKey]); !ok {
		return snapshot, fmt.Errorf("failed to parse %q property", PropertySchemeNetworkKey)
	}

	numbers := []struct {
		key string
		dst *int64
	}{
		{PropertySchemeTotalKey, &snapshot.Total},
		{PropertySchemePreV2Key, &snapshot.PreV2},
		{PropertySchemeV2HalfpathKey, &snapshot.V2Halfpath},
		{PropertySchemeV2FlatKey, &snapshot.V2Flat},
		{PropertySchemeV2OtherKey, &snapshot.V2Other},
		{PropertySchemeStaleKey, &snapshot.Stale},
		{PropertySchemeNetworkELTotalKey, &snapshot.NetworkELTotal},
	}
	for _, n := range numbers {
		if *n.dst, ok = GetNumberValue(page.Properties[n.key]); !ok {
			return snapshot, fmt.Errorf("failed to parse %q property", n.key)
		}
	}

	if snapshot.Methodology, ok = GetRichTextValue(page.Properties[PropertySchemeMethodologyKey]); !ok {
		return snapshot, fmt.Errorf("failed to parse %q property", PropertySchemeMethodologyKey)
	}

	// Snapshot At is informational; an empty date should not make history unreadable.
	snapshot.SnapshotAt, _ = GetDateValue(page.Properties[PropertySchemeSnapshotAtKey])

	if snapshot.CreatedAt, ok = GetCreatedTimeValue(page.Properties[PropertySchemeCreatedTimeKey]); !ok {
		return snapshot, fmt.Errorf("failed to parse %q property", PropertySchemeCreatedTimeKey)
	}

	return snapshot, nil
}
