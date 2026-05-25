package datasources

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly"

	"client-nodes-reporter/configs"
)

// readMaybeGzip reads a response body, decompressing it when the server
// returned Content-Encoding: gzip. net/http only auto-decompresses when
// the caller did NOT set Accept-Encoding explicitly — code paths that set
// it manually (to look browser-like for Cloudflare) must decompress here.
func readMaybeGzip(resp *http.Response) ([]byte, error) {
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		return raw, nil
	}
	gr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()
	return io.ReadAll(gr)
}

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

func (e EthernodesDataSource) getNumbersFromWithContent(url string, clientName configs.ClientType) (int64, int64, string, error) {
	// Total number of clients
	var total int64 = -1
	var clientNumber int64 = -1
	var scrapeErr error

	// Try direct HTTP request first (like curl)
	slog.Debug("Trying direct HTTP request like curl", "url", url)
	
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	
	// Create request with exact same headers as working curl
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return -1, -1, "", fmt.Errorf("failed to create request: %w", err)
	}
	
	// Set the exact same User-Agent as the working curl command
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	
	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		slog.Debug("Direct HTTP request failed", "error", err)
		// Fall back to colly if direct request fails
		total, clientNumber, err := e.getNumbersFromWithColly(url, clientName)
		return total, clientNumber, "", err
	}
	defer resp.Body.Close()
	
	slog.Debug("Direct HTTP request successful", "status", resp.StatusCode, "contentLength", resp.ContentLength)
	
	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Debug("Failed to read response body", "error", err)
		total, clientNumber, err := e.getNumbersFromWithColly(url, clientName)
		return total, clientNumber, "", err
	}
	
	bodyStr := string(body)
	slog.Debug("Response body length", "length", len(bodyStr))
	
	// Check if we got a Cloudflare protection page
	if strings.Contains(bodyStr, "Just a moment") || strings.Contains(bodyStr, "Cloudflare") {
		slog.Debug("Detected Cloudflare protection page in direct request")
		total, clientNumber, err := e.getNumbersFromWithColly(url, clientName)
		return total, clientNumber, "", err
	}
	
	// Parse the HTML using goquery
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(bodyStr))
	if err != nil {
		slog.Debug("Failed to parse HTML from direct request", "error", err)
		total, clientNumber, err := e.getNumbersFromWithColly(url, clientName)
		return total, clientNumber, "", err
	}
	
	// Process the HTML
	processHTML(doc, clientName, &total, &clientNumber, &scrapeErr)
	
	if total > 0 && clientNumber > 0 {
		slog.Debug("Successfully extracted data from direct HTTP request", "total", total, "clientNumber", clientNumber)
		return total, clientNumber, bodyStr, nil
	}
	
	slog.Debug("Direct HTTP request didn't yield data, trying colly fallback")
	total, clientNumber, err = e.getNumbersFromWithColly(url, clientName)
	return total, clientNumber, "", err
}

func (e EthernodesDataSource) getNumbersFromWithColly(url string, clientName configs.ClientType) (int64, int64, error) {
	// Total number of clients
	var total int64 = -1
	var clientNumber int64 = -1
	var scrapeErr error

	c := colly.NewCollector(
		colly.MaxDepth(1),
		colly.AllowURLRevisit(),
	)
	
	// Add a delay between requests to be respectful
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		RandomDelay: 5 * time.Second,
	})

	// Try different user agents to avoid detection
	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/121.0",
	}
	
	// Use a random user agent
	randomIndex := time.Now().UnixNano() % int64(len(userAgents))
	c.UserAgent = userAgents[randomIndex]
	
	c.OnRequest(func(r *colly.Request) {
		// Add headers that might help bypass protection
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
		r.Headers.Set("Accept-Language", "en-US,en;q=0.9")
		r.Headers.Set("Accept-Encoding", "gzip, deflate, br")
		r.Headers.Set("Cache-Control", "max-age=0")
		r.Headers.Set("Sec-Ch-Ua", "\"Not_A Brand\";v=\"8\", \"Chromium\";v=\"120\", \"Google Chrome\";v=\"120\"")
		r.Headers.Set("Sec-Ch-Ua-Mobile", "?0")
		r.Headers.Set("Sec-Ch-Ua-Platform", "\"Windows\"")
		r.Headers.Set("Sec-Fetch-Dest", "document")
		r.Headers.Set("Sec-Fetch-Mode", "navigate")
		r.Headers.Set("Sec-Fetch-Site", "none")
		r.Headers.Set("Sec-Fetch-User", "?1")
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
			if len(bodyStr) > 500 {
				slog.Debug("Response body snippet", "snippet", bodyStr[:500])
			} else {
				slog.Debug("Response body", "body", bodyStr)
			}
			
			// Check if we got a Cloudflare protection page
			if strings.Contains(bodyStr, "Just a moment") || strings.Contains(bodyStr, "Cloudflare") {
				slog.Debug("Detected Cloudflare protection page - this source may not be accessible")
				// Don't try to process Cloudflare protection pages
				return
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
		if retries < 1 { // Reduce max retries to 1 to prevent infinite loops
			slog.Debug("Error during http request. Retrying...", "error", err, "retries", retries)
			delay := time.Duration(int64(e.config.InitialRetryDelay) * (1 << uint(retries)))
			time.Sleep(delay)
			r.Ctx.Put("retries", retries+1)
			r.Request.Retry()
		} else {
			slog.Debug("Max retries reached, giving up", "error", err, "retries", retries)
		}
	})

	// Try different HTTP methods
	methods := []string{"GET", "HEAD"}
	var visitErr error
	
	for _, method := range methods {
		if method == "GET" {
			visitErr = c.Visit(url)
		} else {
			// For HEAD requests, we need to handle differently
			req, err := http.NewRequest(method, url, nil)
			if err != nil {
				continue
			}
			req.Header.Set("User-Agent", c.UserAgent)
			req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
			
			client := &http.Client{Timeout: 30 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			resp.Body.Close()
			
			if resp.StatusCode == 200 {
				// If HEAD works, try GET
				visitErr = c.Visit(url)
				break
			}
		}
		
		if visitErr == nil {
			break
		}
	}
	
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
		// Check if this is likely a Cloudflare protection issue
		if strings.Contains(visitErr.Error(), "Forbidden") {
			return -1, -1, fmt.Errorf("access denied by Cloudflare protection - ethernodes.org may not be accessible via automated requests")
		}
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
	// First, get the correct total counts from the main page
	mainURLs := []string{
		"https://ethernodes.org/",
		"https://ethernodes.org",
		"https://www.ethernodes.org/",
		"https://www.ethernodes.org",
	}

	var total int64 = -1
	var clientTotal int64 = -1
	var lastErr error

	// Try to get data from main page first
	for _, url := range mainURLs {
		slog.Debug("Trying main page for total counts", "url", url)
		overallTotal, clientNumber, _, err := e.getNumbersFromWithContent(url, clientName)
		if err == nil && overallTotal > 0 && clientNumber > 0 {
			total = overallTotal
			clientTotal = clientNumber
			slog.Info("Successfully retrieved total counts from main page", "url", url, "total", total, "clientTotal", clientTotal)
			break
		}
		lastErr = err
		slog.Debug("Failed to get data from", "url", url, "error", err)
		time.Sleep(2 * time.Second)
	}

	if total <= 0 || clientTotal <= 0 {
		return ClientData{}, fmt.Errorf("failed to get total counts from any ethernodes.org endpoint: %w", lastErr)
	}

	// Per-client synced count from /client/el/<name>?synced=1.
	clientSynced, err := e.getClientSyncedCount(clientName)
	if err != nil {
		return ClientData{}, fmt.Errorf("failed to get client synced count: %w", err)
	}
	if clientSynced > clientTotal {
		// Defensive: cap at clientTotal so downstream math stays sensible.
		slog.Warn("clientSynced exceeds clientTotal, capping", "clientSynced", clientSynced, "clientTotal", clientTotal)
		clientSynced = clientTotal
	}

	// Overall EL synced count from /sync.
	totalSynced, err := e.getOverallExecutionLayerSynced()
	if err != nil {
		return ClientData{}, fmt.Errorf("failed to get overall synced count: %w", err)
	}

	slog.Info("Successfully retrieved ethernodes data",
		"client", clientName,
		"clientTotal", clientTotal,
		"clientSynced", clientSynced,
		"overallTotal", total,
		"overallSynced", totalSynced)

	return ClientData{
		Source:       string(e.SourceType()),
		ClientName:   clientName,
		Total:        total,
		ClientTotal:  clientTotal,
		TotalSynced:  totalSynced,
		ClientSynced: clientSynced,
		CreatedAt:    time.Now(),
	}, nil
}

// getClientURLName maps our client types to Ethernodes URL format
func (e EthernodesDataSource) getClientURLName(clientName configs.ClientType) string {
	switch clientName {
	case configs.ClientTypeNethermind:
		return "nethermind"
	case configs.ClientTypeGeth:
		return "geth"
	case configs.ClientTypeBesu:
		return "besu"
	case configs.ClientTypeErigon:
		return "erigon"
	case configs.ClientTypeReth:
		return "reth"
	default:
		return ""
	}
}

// getClientCountFromURL gets the count of nodes from a specific client URL
func (e EthernodesDataSource) getClientCountFromURL(url string) (int64, error) {
	// Try direct HTTP request first (like curl)
	slog.Debug("Trying direct HTTP request for client count", "url", url)
	
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	
	// Create request with exact same headers as working curl
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return -1, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Set the exact same User-Agent as the working curl command
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	
	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		slog.Debug("Direct HTTP request failed", "error", err)
		return -1, err
	}
	defer resp.Body.Close()
	
	slog.Debug("Direct HTTP request successful", "status", resp.StatusCode, "contentLength", resp.ContentLength)
	
	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Debug("Failed to read response body", "error", err)
		return -1, err
	}
	
	bodyStr := string(body)
	slog.Debug("Response body length", "length", len(bodyStr))
	
	// Check if we got a Cloudflare protection page
	if strings.Contains(bodyStr, "Just a moment") || strings.Contains(bodyStr, "Cloudflare") {
		slog.Debug("Detected Cloudflare protection page in direct request")
		return -1, fmt.Errorf("cloudflare protection detected")
	}
	
	// Parse the HTML using goquery
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(bodyStr))
	if err != nil {
		slog.Debug("Failed to parse HTML from direct request", "error", err)
		return -1, err
	}
	
	// Look for the count in the page
	// Ethernodes client pages typically show the count in a prominent location
	var count int64 = -1
	
	// Try different selectors to find the count
	selectors := []string{
		".node-count",
		".count",
		"h1", // Sometimes the count is in the main heading
		".stats-number",
		"[data-count]",
	}
	
	for _, selector := range selectors {
		doc.Find(selector).Each(func(i int, s *goquery.Selection) {
			text := strings.TrimSpace(s.Text())
			// Look for numbers in the text
			if strings.Contains(text, "node") || strings.Contains(text, "count") {
				// Extract numbers from the text
				words := strings.Fields(text)
				for _, word := range words {
					// Remove common punctuation
					word = strings.Trim(word, ".,!?()[]{}")
					if parsed, err := strconv.ParseInt(word, 10, 64); err == nil && parsed > 0 {
						count = parsed
						slog.Debug("Found count from selector", "selector", selector, "text", text, "count", count)
						return
					}
				}
			}
		})
		if count > 0 {
			break
		}
	}
	
	if count > 0 {
		slog.Debug("Successfully extracted count from client URL", "url", url, "count", count)
		return count, nil
	}
	
	// If we couldn't find the count, try to parse the page content more broadly
	slog.Debug("Could not find count with specific selectors, trying broader search")
	
	// Look for any large numbers in the page that might be the count
	doc.Find("body").Each(func(i int, s *goquery.Selection) {
		text := s.Text()
		// Use regex to find numbers
		re := regexp.MustCompile(`\b(\d{1,3}(?:,\d{3})*)\b`)
		matches := re.FindAllString(text, -1)
		for _, match := range matches {
			// Remove commas and parse
			cleanMatch := strings.ReplaceAll(match, ",", "")
			if parsed, err := strconv.ParseInt(cleanMatch, 10, 64); err == nil && parsed > 0 {
				// Prefer larger numbers as they're more likely to be the count
				if parsed > count {
					count = parsed
				}
			}
		}
	})
	
	if count > 0 {
		slog.Debug("Found count using broader search", "url", url, "count", count)
		return count, nil
	}
	
	return -1, fmt.Errorf("could not extract count from URL: %s", url)
}

// getClientSyncedCount returns the count of synced nodes for one client, as
// reported by https://ethernodes.org/client/el/<name>?synced=1.
// (?synced=0 also exists but does not filter — it returns the same page as no
// query parameter, so we ignore it and derive unsynced = total - synced.)
func (e EthernodesDataSource) getClientSyncedCount(clientName configs.ClientType) (int64, error) {
	clientURLName := e.getClientURLName(clientName)
	if clientURLName == "" {
		return -1, fmt.Errorf("unsupported client: %s", clientName)
	}

	syncedURL := fmt.Sprintf("https://ethernodes.org/client/el/%s?synced=1", clientURLName)
	slog.Debug("Fetching client synced count", "url", syncedURL)
	return e.getClientCountWithEnhancedHeaders(syncedURL)
}

// getOverallExecutionLayerSynced returns the overall synced EL node count from
// https://ethernodes.org/sync (the "Execution Layer Sync Status" section).
func (e EthernodesDataSource) getOverallExecutionLayerSynced() (int64, error) {
	const syncURL = "https://ethernodes.org/sync"
	slog.Debug("Fetching overall execution-layer synced count", "url", syncURL)

	body, err := e.fetchWithEnhancedHeaders(syncURL)
	if err != nil {
		return -1, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return -1, fmt.Errorf("parse /sync HTML: %w", err)
	}

	// The page has two sections (Execution Layer, Consensus Layer), each with
	// progress-groups labelled Total/Synced/Syncing. The first "Synced" group
	// in DOM order belongs to Execution Layer.
	count := findProgressGroupTotal(doc, "synced")
	if count <= 0 {
		return -1, fmt.Errorf("could not extract synced count from %s", syncURL)
	}
	return count, nil
}

// fetchWithEnhancedHeaders performs the same browser-like GET that
// getClientCountWithEnhancedHeaders does, but returns the decompressed body so
// other scrapers can reuse the bypass.
func (e EthernodesDataSource) fetchWithEnhancedHeaders(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Sec-Ch-Ua", "\"Not_A Brand\";v=\"8\", \"Chromium\";v=\"120\", \"Google Chrome\";v=\"120\"")
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", "\"macOS\"")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Referer", "https://ethernodes.org/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := readMaybeGzip(resp)
	if err != nil {
		return nil, err
	}
	if strings.Contains(string(body), "Just a moment") || strings.Contains(string(body), "cf-browser-verification") {
		return nil, fmt.Errorf("cloudflare protection detected at %s", url)
	}
	return body, nil
}


// getClientCountWithEnhancedHeaders fetches one of the per-client Ethernodes
// pages and extracts the "Total" count from its first .progress-group.
func (e EthernodesDataSource) getClientCountWithEnhancedHeaders(url string) (int64, error) {
	body, err := e.fetchWithEnhancedHeaders(url)
	if err != nil {
		return -1, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return -1, fmt.Errorf("parse HTML from %s: %w", url, err)
	}
	count := findProgressGroupTotal(doc, "total")
	if count <= 0 {
		return -1, fmt.Errorf("could not extract count from %s", url)
	}
	slog.Debug("Successfully extracted count with enhanced headers", "url", url, "count", count)
	return count, nil
}

// findProgressGroupTotal locates the count value displayed alongside a labelled
// row inside a Bootstrap-style `.progress-group` block. The Ethernodes per-client
// pages render their headline numbers as:
//
//	<div class="progress-group">
//	  <div class="progress-group-header ...">
//	    <div>Total</div>
//	    <div class="ms-auto fw-semibold me-2">1396</div>
//	  </div>
//	</div>
//
// Pass `labelLower` lowercased (e.g. "total", "synced", "syncing").
func findProgressGroupTotal(doc *goquery.Document, labelLower string) int64 {
	var count int64 = -1
	doc.Find(".progress-group").EachWithBreak(func(_ int, pg *goquery.Selection) bool {
		header := pg.Find(".progress-group-header").First()
		if header.Length() == 0 {
			return true
		}
		if !strings.Contains(strings.ToLower(header.Text()), labelLower) {
			return true
		}
		text := strings.TrimSpace(header.Find(".fw-semibold").First().Text())
		cleaned := strings.ReplaceAll(text, ",", "")
		if parsed, err := strconv.ParseInt(cleaned, 10, 64); err == nil && parsed > 0 {
			count = parsed
			return false
		}
		return true
	})
	return count
} 
