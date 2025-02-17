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

const EthernetsSourceName = "Ethernets"

type EthernetsDataSourceOptions struct {
	BaseURL           string
	MaxRetries        int
	InitialRetryDelay time.Duration
}

type EthernetsDataSource struct {
	config EthernetsDataSourceOptions
}

func NewEthernetsDataSource(cfg *EthernetsDataSourceOptions) (*EthernetsDataSource, error) {
	config := EthernetsDataSourceOptions{
		BaseURL:           "https://www.ethernets.io",
		MaxRetries:        3,
		InitialRetryDelay: 1 * time.Second,
	}

	if cfg != nil {
		if cfg.BaseURL != "" {
			config.BaseURL = cfg.BaseURL
		}
		if cfg.MaxRetries < 0 {
			config.MaxRetries = 3
		} else {
			config.MaxRetries = cfg.MaxRetries
		}
		if cfg.InitialRetryDelay < 0 {
			config.InitialRetryDelay = 1 * time.Second
		} else {
			config.InitialRetryDelay = cfg.InitialRetryDelay
		}
	}

	return &EthernetsDataSource{config: config}, nil
}

func (e EthernetsDataSource) SourceType() DataSourceType {
	return DataSourceTypeEthernets
}

func (e EthernetsDataSource) SourceName() string {
	return EthernetsSourceName
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
		r.Ctx.Put("retries", 0)
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

	c.OnError(func(r *colly.Response, err error) {
		retries := r.Ctx.GetAny("retries").(int)
		if retries < e.config.MaxRetries {
			slog.Info("Error during http request. Retrying...", "error", err, "retries", retries)
			delay := time.Duration(int64(e.config.InitialRetryDelay) * (1 << uint(retries)))
			time.Sleep(delay)
			r.Ctx.Put("retries", retries+1)
			r.Request.Retry()
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
	syncedUrl := fmt.Sprintf("%s/?synced=yes", e.config.BaseURL)
	unsyncedUrl := fmt.Sprintf("%s/?synced=no", e.config.BaseURL)

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
