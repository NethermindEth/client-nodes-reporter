package datasources

import (
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly"

	"client-nodes-reporter/configs"
)

type EthernetsDataSource struct{}

const (
	EthernetsURL  = "https://www.ethernets.io"
	EthernetsName = "Ethernets"
)

func (e EthernetsDataSource) SourceType() DataSourceType {
	return DataSourceTypeEthernets
}

func (e EthernetsDataSource) SourceName() string {
	return EthernetsName
}

func (e EthernetsDataSource) getNumbersFrom(url string, clientName configs.ClientType) (int64, int64, error) {
	// Total number of clients
	var total int64 = -1
	var clientNumber int64 = -1
	var scrapeErr error

	c := colly.NewCollector(
		colly.MaxDepth(1),
	)

	c.OnRequest(func(r *colly.Request) {
		slog.Debug("Visiting", "url", r.URL)
	})

	c.OnHTML("h2", func(e *colly.HTMLElement) {
		if strings.Contains(e.Text, "Client Names") {
			// Get the parent div element
			parent := e.DOM.Parent()

			// All clients have text in the format `<client-name> (<number>)` with a special case for `Total`
			parent.Find("span").Each(func(i int, s *goquery.Selection) {
				rawClientText := s.Text()
				re := regexp.MustCompile(`(\w+)\s+\((\d+)\)`)
				matches := re.FindStringSubmatch(rawClientText)
				if len(matches) == 3 {
					parsedName := matches[1]
					parsedNumber := matches[2]
					slog.Debug("Found client", "name", parsedName, "number", parsedNumber)
					if strings.EqualFold(parsedName, "Total") {
						totalParsed, err := strconv.ParseInt(parsedNumber, 10, 64)
						if err != nil {
							scrapeErr = fmt.Errorf("failed to parse total number of clients: %w", err)
							return
						}
						total = totalParsed
					} else if strings.EqualFold(parsedName, string(clientName)) {
						clientNumberParsed, err := strconv.ParseInt(parsedNumber, 10, 64)
						if err != nil {
							scrapeErr = fmt.Errorf("failed to parse client number: %w", err)
							return
						}
						clientNumber = clientNumberParsed
					}
				}
			})
		}
	})

	if err := c.Visit(url); err != nil {
		return -1, -1, err
	}

	if scrapeErr != nil {
		return total, clientNumber, fmt.Errorf("failed to find total or client data: %w", scrapeErr)
	}

	return total, clientNumber, nil
}

func (e EthernetsDataSource) GetClientData(clientName configs.ClientType) (ClientData, error) {
	syncedUrl := fmt.Sprintf("%s/?synced=yes", EthernetsURL)
	unsyncedUrl := fmt.Sprintf("%s/?synced=no", EthernetsURL)

	totalSynced, clientSynced, err := e.getNumbersFrom(syncedUrl, clientName)
	if err != nil {
		return ClientData{}, fmt.Errorf("failed to get synced data: %w", err)
	}
	totalUnsynced, clientUnsynced, err := e.getNumbersFrom(unsyncedUrl, clientName)
	if err != nil {
		return ClientData{}, fmt.Errorf("failed to get unsynced data: %w", err)
	}

	totalNumber := totalSynced + totalUnsynced

	clientTotal := clientSynced + clientUnsynced

	return ClientData{
		string(e.SourceType()),
		clientName,
		totalNumber,
		clientTotal,
		totalSynced,
		clientSynced,
		time.Now(),
	}, nil
}
