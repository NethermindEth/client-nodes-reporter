package datasources

import (
	"time"

	"client-nodes-reporter/configs"
)

type DataSourceType string

const (
	DataSourceTypeEthernets DataSourceType = "ethernets"
)

type ClientData struct {
	Source       string
	ClientName   configs.ClientType
	Total        int64
	ClientTotal  int64
	TotalSynced  int64
	ClientSynced int64
	CreatedAt    time.Time
}

func (c ClientData) Compare(other ClientData) int {
	return c.CreatedAt.Compare(other.CreatedAt)
}

type DataSource interface {
	SourceName() string
	SourceType() DataSourceType
	GetClientData(clientName string) (ClientData, error)
}
