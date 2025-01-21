package database

import (
	"client-nodes-reporter/datasources"
	"context"

	"github.com/jomei/notionapi"
)

type NotionDBOptions struct {
	DatabaseID string
	Token      string
}

type NotionDB struct {
	client   *notionapi.Client
	database *notionapi.Database
}

func (db *NotionDB) GetLatestData(
	client string,
	pageSize int,
	source datasources.DataSourceType,
) ([]datasources.ClientData, error) {
	response, err := db.client.Database.Query(
		context.Background(),
		notionapi.DatabaseID(db.database.ID),
		&notionapi.DatabaseQueryRequest{
			Filter: notionapi.AndCompoundFilter{
				&notionapi.PropertyFilter{
					Property: PropertySourceKey,
					Select: &notionapi.SelectFilterCondition{
						Equals: string(source),
					},
				},
				&notionapi.PropertyFilter{
					Property: PropertyClientTypeKey,
					Select: &notionapi.SelectFilterCondition{
						Equals: client,
					},
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

	pages := response.Results
	latestData := make([]datasources.ClientData, 0, len(pages))
	for _, page := range pages {
		clientData, err := PageToClientData(&page)
		if err != nil {
			return nil, err
		}

		latestData = append(latestData, clientData)
	}

	return latestData, nil
}

func (db *NotionDB) AddClientData(clientData datasources.ClientData) error {
	pageProperties, err := ClientDataToPageProperties(clientData)
	if err != nil {
		return err
	}

	_, err = db.client.Page.Create(
		context.Background(),
		&notionapi.PageCreateRequest{
			Parent: notionapi.Parent{
				DatabaseID: notionapi.DatabaseID(db.database.ID.String()),
			},
			Properties: pageProperties,
		},
	)
	if err != nil {
		return err
	}

	return nil
}

func NewNotionDB(options NotionDBOptions) (*NotionDB, error) {
	notionClient := notionapi.NewClient(notionapi.Token(options.Token))
	database, err := notionClient.Database.Get(context.Background(), notionapi.DatabaseID(options.DatabaseID))
	if err != nil {
		return nil, err
	}

	return &NotionDB{
		notionClient,
		database,
	}, nil
}
