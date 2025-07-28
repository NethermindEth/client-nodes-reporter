package datasources

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly"

	"client-nodes-reporter/configs"
)

const EthernodesSourceName = "Ethernodes"

type EthernodesDataSourceOptions struct {
	BaseURL           string
	MaxRetries        int
	InitialRetryDelay time.Duration
}

type EthernodesDataSource struct {
	config EthernodesDataSourceOptions
}

func NewEthernodesDataSource(cfg *EthernodesDataSourceOptions) (*EthernodesDataSource, error) {
	config := EthernodesDataSourceOptions{
		BaseURL:           "https://ethernodes.org",
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

	return &EthernodesDataSource{config: config}, nil
}

func (e EthernodesDataSource) SourceType() DataSourceType {
	return DataSourceTypeEthernodes
}

func (e EthernodesDataSource) SourceName() string {
	return EthernodesSourceName
}

func (e EthernodesDataSource) getNumbersFrom(url string, clientName configs.ClientType) (int64, int64, error) {
	// Total number of clients
	var total int64 = -1
	var clientNumber int64 = -1
	var scrapeErr error

	c := colly.NewCollector(
		colly.MaxDepth(1),
	)
	
	// Add a delay between requests to be respectful
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		RandomDelay: 2 * time.Second,
	})

	// Set user agent and headers to avoid protection
	c.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	
	c.OnRequest(func(r *colly.Request) {
		// Add headers that might help bypass protection
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
		r.Headers.Set("Accept-Language", "en-US,en;q=0.5")
		r.Headers.Set("Accept-Encoding", "gzip, deflate")
		r.Headers.Set("Connection", "keep-alive")
		r.Headers.Set("Upgrade-Insecure-Requests", "1")
		
		slog.Debug("Visiting", "url", r.URL)
		r.Ctx.Put("retries", 0)
	})

	// Look for Execution Layer Clients section
	c.OnHTML("h4", func(e *colly.HTMLElement) {
		if strings.Contains(e.Text, "Execution Layer Clients") {
			slog.Debug("Found Execution Layer Clients section")
			processHTMLFromSelection(e.DOM, clientName, &total, &clientNumber, &scrapeErr)
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		slog.Debug("HTTP Error", "status", r.StatusCode, "error", err, "url", r.Request.URL)
		
		// If we got a 403 Forbidden, try to process the response anyway
		if r.StatusCode == 403 {
			slog.Debug("Received 403 Forbidden, attempting to process response body", "bodyLength", len(r.Body))
			
			// Log a snippet of the response body to see what we're getting
			bodyStr := string(r.Body)
			if len(bodyStr) > 200 {
				slog.Debug("Response body snippet", "snippet", bodyStr[:200])
			} else {
				slog.Debug("Response body", "body", bodyStr)
			}
			
			// Try to parse the response body even with 403
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(bodyStr))
			if err != nil {
				slog.Debug("Failed to parse response body", "error", err)
				return
			}
			
			slog.Debug("Successfully parsed response body, processing HTML")
			// Process the HTML manually
			processHTML(doc, clientName, &total, &clientNumber, &scrapeErr)
			slog.Debug("Finished processing HTML from 403 response", "total", total, "clientNumber", clientNumber)
			return
		}
		
		retries := r.Ctx.GetAny("retries").(int)
		if retries < e.config.MaxRetries {
			slog.Info("Error during http request. Retrying...", "error", err, "retries", retries)
			delay := time.Duration(int64(e.config.InitialRetryDelay) * (1 << uint(retries)))
			time.Sleep(delay)
			r.Ctx.Put("retries", retries+1)
			r.Request.Retry()
		}
	})

	visitErr := c.Visit(url)
	
	// Check if we got data despite any errors
	if total > 0 && clientNumber > 0 {
		slog.Debug("Got data despite error, proceeding with available data", "total", total, "clientNumber", clientNumber, "visitError", visitErr)
		return total, clientNumber, nil
	}
	
	// If we got some data but not complete, log it
	if total > 0 || clientNumber > 0 {
		slog.Debug("Got partial data", "total", total, "clientNumber", clientNumber, "visitError", visitErr)
	}
	
	if visitErr != nil {
		return -1, -1, visitErr
	}

	if scrapeErr != nil {
		return total, clientNumber, fmt.Errorf("failed to find total or client data: %w", scrapeErr)
	}

	return total, clientNumber, nil
}

// processHTML extracts client data from the HTML document
func processHTML(doc *goquery.Document, clientName configs.ClientType, total *int64, clientNumber *int64, scrapeErr *error) {
	// Look for Execution Layer Clients section
	doc.Find("h4").Each(func(i int, s *goquery.Selection) {
		if strings.Contains(s.Text(), "Execution Layer Clients") {
			processHTMLFromSelection(s, clientName, total, clientNumber, scrapeErr)
		}
	})
}

// processHTMLFromSelection extracts client data from a goquery selection
func processHTMLFromSelection(s *goquery.Selection, clientName configs.ClientType, total *int64, clientNumber *int64, scrapeErr *error) {
	slog.Debug("Processing HTML from selection", "clientName", clientName)
	
	// Find the parent container that holds all progress groups
	parent := s.Parent()
	slog.Debug("Found parent container", "parentLength", parent.Length())
	
	// Look for progress groups within this section
	progressGroups := parent.Find(".progress-group")
	slog.Debug("Found progress groups", "count", progressGroups.Length())
	
	// Process each progress group
	progressGroups.Each(func(i int, s *goquery.Selection) {
		// Get the client name and count from the progress group header
		header := s.Find(".progress-group-header")
		
		// Extract client name
		clientNameElement := header.Find("div").First()
		clientNameText := strings.TrimSpace(clientNameElement.Text())
		
		// Skip if it's the "Total" row
		if strings.EqualFold(clientNameText, "Total") {
			// Extract total count
			countElement := header.Find(".fw-semibold").First()
			if countElement.Length() > 0 {
				totalText := strings.TrimSpace(countElement.Text())
				totalParsed, err := strconv.ParseInt(totalText, 10, 64)
				if err != nil {
					*scrapeErr = fmt.Errorf("failed to parse total number: %w", err)
					return
				}
				*total = totalParsed
				slog.Debug("Found total", "total", *total)
			}
			return
		}
		
		// Extract client count
		countElement := header.Find(".fw-semibold").First()
		if countElement.Length() > 0 {
			countText := strings.TrimSpace(countElement.Text())
			countParsed, err := strconv.ParseInt(countText, 10, 64)
			if err != nil {
				*scrapeErr = fmt.Errorf("failed to parse client count: %w", err)
				return
			}
			
			// Check if this is the client we're looking for
			// Ethernodes uses different naming conventions than our configs
			if matchesClientName(clientNameText, clientName) {
				*clientNumber = countParsed
				slog.Debug("Found client", "name", clientNameText, "count", *clientNumber)
			}
		}
	})
}

// matchesClientName maps our client types to Ethernodes naming conventions
func matchesClientName(ethernodesName string, clientName configs.ClientType) bool {
	ethernodesName = strings.ToLower(strings.TrimSpace(ethernodesName))
	
	switch clientName {
	case configs.ClientTypeGeth:
		// Ethernodes shows "geth" and "go-ethereum" as separate clients
		// We'll count both as "geth" for our purposes
		return ethernodesName == "geth" || ethernodesName == "go-ethereum"
	case configs.ClientTypeNethermind:
		return ethernodesName == "nethermind"
	case configs.ClientTypeBesu:
		return ethernodesName == "besu"
	case configs.ClientTypeErigon:
		return ethernodesName == "erigon"
	case configs.ClientTypeReth:
		return ethernodesName == "reth"
	default:
		return false
	}
}

func (e EthernodesDataSource) GetClientData(clientName configs.ClientType) (ClientData, error) {
	// Ethernodes.org shows all nodes as synced (they only show active nodes)
	// So we'll treat all nodes as synced for this data source
	total, clientTotal, err := e.getNumbersFrom(e.config.BaseURL, clientName)
	if err != nil {
		return ClientData{}, fmt.Errorf("failed to get client data: %w", err)
	}

	// For Ethernodes, we assume all nodes are synced since they only show active nodes
	totalSynced := total
	clientSynced := clientTotal

	return ClientData{
		string(e.SourceType()),
		clientName,
		total,
		clientTotal,
		totalSynced,
		clientSynced,
		time.Now(),
	}, nil
} 