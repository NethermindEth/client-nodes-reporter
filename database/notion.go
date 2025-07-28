package database

import (
	"fmt"

	"github.com/jomei/notionapi"

	"client-nodes-reporter/configs"
	"client-nodes-reporter/datasources"
)

const (
	PropertyNameKey         = "Name"
	PropertyTotalKey        = "Total"
	PropertyClientTotalKey  = "Client Total"
	PropertyTotalSyncedKey  = "Total Synced"
	PropertyClientSyncedKey = "Client Synced"
	PropertyClientTypeKey   = "Client"
	PropertySourceKey       = "Source"
	PropertyCreatedTimeKey  = "Created time"
)

func PageToClientData(page *notionapi.Page) (datasources.ClientData, error) {
	source, ok := GetSelectValue(page.Properties[PropertySourceKey])
	if !ok {
		return datasources.ClientData{}, fmt.Errorf("failed to parse source property")
	}

	clientName, ok := GetSelectValue(page.Properties[PropertyClientTypeKey])
	if !ok {
		return datasources.ClientData{}, fmt.Errorf("failed to parse client type property")
	}

	total, ok := GetNumberValue(page.Properties[PropertyTotalKey])
	if !ok {
		return datasources.ClientData{}, fmt.Errorf("failed to parse total property")
	}

	clientTotal, ok := GetNumberValue(page.Properties[PropertyClientTotalKey])
	if !ok {
		return datasources.ClientData{}, fmt.Errorf("failed to parse client total property")
	}

	totalSynced, ok := GetNumberValue(page.Properties[PropertyTotalSyncedKey])
	if !ok {
		return datasources.ClientData{}, fmt.Errorf("failed to parse total synced property")
	}

	clientSynced, ok := GetNumberValue(page.Properties[PropertyClientSyncedKey])
	if !ok {
		return datasources.ClientData{}, fmt.Errorf("failed to parse client synced property")
	}

	createdAt, ok := GetCreatedTimeValue(page.Properties[PropertyCreatedTimeKey])
	if !ok {
		return datasources.ClientData{}, fmt.Errorf("failed to parse created time property")
	}

	return datasources.ClientData{
		Source:       source,
		ClientName:   configs.ClientTypeFromString(clientName),
		Total:        total,
		ClientTotal:  clientTotal,
		TotalSynced:  totalSynced,
		ClientSynced: clientSynced,
		CreatedAt:    createdAt,
	}, nil
}

func ClientDataToPageProperties(clientData datasources.ClientData) (notionapi.Properties, error) {
	pageProperties := make(notionapi.Properties, 10)

	pageProperties[PropertyNameKey] = BuildTitleProperty(fmt.Sprintf("%s-%s", clientData.Source, clientData.ClientName))
	pageProperties[PropertySourceKey] = BuildSelectProperty(clientData.Source)
	
	// Map client names to Notion database values
	clientName := string(clientData.ClientName) // Use lowercase as stored in configs
	pageProperties[PropertyClientTypeKey] = BuildSelectProperty(clientName)
	
	pageProperties[PropertyTotalKey] = BuildNumberProperty(float64(clientData.Total))
	pageProperties[PropertyClientTotalKey] = BuildNumberProperty(float64(clientData.ClientTotal))
	pageProperties[PropertyTotalSyncedKey] = BuildNumberProperty(float64(clientData.TotalSynced))
	pageProperties[PropertyClientSyncedKey] = BuildNumberProperty(float64(clientData.ClientSynced))

	return pageProperties, nil
}
