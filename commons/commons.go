// Package commons is the library behind the commons command line:
// the HTTP client, request shaping, wire types, and the typed data models
// for Wikimedia Commons.
//
// The Client is the spine every command shares. It sets a real User-Agent
// (required by Wikimedia policy), paces requests so a busy session stays
// polite, and retries the transient failures (429 and 5xx) that any public
// API throws under load. Build your endpoint calls and JSON decoding on top
// of it.
package commons

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// APIBase is the Wikimedia Commons MediaWiki API endpoint.
const APIBase = "https://commons.wikimedia.org/w/api.php"

// Host is the commons hostname used for URI matching in domain.go.
const Host = "commons.wikimedia.org"

// BaseURL is the root of the wiki, used to build human-readable page URLs.
const BaseURL = "https://" + Host

// Config holds tunable parameters for the Client.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Timeout   time.Duration
	Retries   int
}

// DefaultConfig returns the recommended Config for production use.
func DefaultConfig() Config {
	return Config{
		BaseURL:   APIBase,
		UserAgent: "commons-cli/0.1.0 (github.com/tamnd/commons-cli)",
		Rate:      500 * time.Millisecond,
		Timeout:   15 * time.Second,
		Retries:   3,
	}
}

// Client talks to the Wikimedia Commons API over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	BaseURL   string
	Rate      time.Duration
	Retries   int

	last time.Time
}

// NewClient returns a Client from cfg.
func NewClient(cfg Config) *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: cfg.Timeout},
		UserAgent: cfg.UserAgent,
		BaseURL:   cfg.BaseURL,
		Rate:      cfg.Rate,
		Retries:   cfg.Retries,
	}
}

// get fetches url and returns the response body. It paces and retries
// according to the client's settings.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// ---------- public output types ----------

// File is a Wikimedia Commons media file.
// FileName is the bare name without "File:" prefix; it doubles as the kit id.
type File struct {
	FileName    string `json:"file_name" kit:"id"`
	Title       string `json:"title"`
	PageID      int    `json:"page_id"`
	URL         string `json:"url,omitempty"`
	Size        int    `json:"size,omitempty"`
	MediaType   string `json:"media_type,omitempty"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	License     string `json:"license,omitempty"`
	Date        string `json:"date,omitempty"`
}

// Category is an entry returned by the category-members list.
type Category struct {
	Title  string `json:"title"`
	PageID int    `json:"page_id"`
	NS     int    `json:"ns"`
}

// ---------- wire types ----------

type wireSearchResponse struct {
	Query struct {
		SearchInfo struct {
			TotalHits int `json:"totalhits"`
		} `json:"searchinfo"`
		Search []struct {
			Title  string `json:"title"`
			PageID int    `json:"pageid"`
		} `json:"search"`
	} `json:"query"`
}

type wireFileResponse struct {
	Query struct {
		Pages map[string]struct {
			Title     string `json:"title"`
			PageID    int    `json:"pageid"`
			ImageInfo []struct {
				URL       string `json:"url"`
				Size      int    `json:"size"`
				MediaType string `json:"mediatype"`
				ExtMeta   map[string]struct {
					Value string `json:"value"`
				} `json:"extmetadata"`
			} `json:"imageinfo"`
		} `json:"pages"`
	} `json:"query"`
}

type wireCategoryResponse struct {
	Query struct {
		CategoryMembers []struct {
			Title  string `json:"title"`
			PageID int    `json:"pageid"`
			NS     int    `json:"ns"`
		} `json:"categorymembers"`
	} `json:"query"`
}

type wireRecentResponse struct {
	Query struct {
		RecentChanges []struct {
			Title     string `json:"title"`
			PageID    int    `json:"pageid"`
			Timestamp string `json:"timestamp"`
		} `json:"recentchanges"`
	} `json:"query"`
}

// ---------- API methods ----------

// SearchFiles searches Wikimedia Commons files by keyword.
// limit controls how many results to return; offset enables pagination.
func (c *Client) SearchFiles(ctx context.Context, query string, limit, offset int) ([]*File, error) {
	if limit <= 0 {
		limit = 20
	}
	params := url.Values{
		"action":      {"query"},
		"list":        {"search"},
		"srsearch":    {query},
		"srnamespace": {"6"},
		"srlimit":     {fmt.Sprintf("%d", limit)},
		"sroffset":    {fmt.Sprintf("%d", offset)},
		"format":      {"json"},
	}
	rawURL := c.BaseURL + "?" + params.Encode()
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	var wire wireSearchResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("search decode: %w", err)
	}
	out := make([]*File, 0, len(wire.Query.Search))
	for _, s := range wire.Query.Search {
		out = append(out, &File{
			FileName: fileBaseName(s.Title),
			Title:    s.Title,
			PageID:   s.PageID,
		})
	}
	return out, nil
}

// GetFile fetches file info and metadata for a single file.
// title should be the bare filename without "File:" prefix; the prefix is
// added automatically.
func (c *Client) GetFile(ctx context.Context, title string) (*File, error) {
	if !strings.HasPrefix(title, "File:") {
		title = "File:" + title
	}
	params := url.Values{
		"action":  {"query"},
		"titles":  {title},
		"prop":    {"imageinfo"},
		"iiprop":  {"url|size|mediatype|extmetadata"},
		"format":  {"json"},
	}
	rawURL := c.BaseURL + "?" + params.Encode()
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	var wire wireFileResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("file decode: %w", err)
	}
	for _, page := range wire.Query.Pages {
		f := &File{
			FileName: fileBaseName(page.Title),
			Title:    page.Title,
			PageID:   page.PageID,
		}
		if len(page.ImageInfo) > 0 {
			ii := page.ImageInfo[0]
			f.URL = ii.URL
			f.Size = ii.Size
			f.MediaType = ii.MediaType
			if v, ok := ii.ExtMeta["ImageDescription"]; ok {
				f.Description = stripHTML(v.Value)
			}
			if v, ok := ii.ExtMeta["Artist"]; ok {
				f.Author = stripHTML(v.Value)
			}
			if v, ok := ii.ExtMeta["LicenseShortName"]; ok {
				f.License = v.Value
			}
			if v, ok := ii.ExtMeta["DateTime"]; ok {
				f.Date = v.Value
			}
		}
		return f, nil
	}
	return nil, fmt.Errorf("file not found: %s", title)
}

// ListCategory lists files that belong to a Wikimedia Commons category.
// name should be the bare category name without "Category:" prefix.
func (c *Client) ListCategory(ctx context.Context, name string, limit int) ([]*Category, error) {
	if limit <= 0 {
		limit = 20
	}
	if !strings.HasPrefix(name, "Category:") {
		name = "Category:" + name
	}
	params := url.Values{
		"action":      {"query"},
		"list":        {"categorymembers"},
		"cmtitle":     {name},
		"cmnamespace": {"6"},
		"cmlimit":     {fmt.Sprintf("%d", limit)},
		"format":      {"json"},
	}
	rawURL := c.BaseURL + "?" + params.Encode()
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	var wire wireCategoryResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("category decode: %w", err)
	}
	out := make([]*Category, 0, len(wire.Query.CategoryMembers))
	for _, m := range wire.Query.CategoryMembers {
		out = append(out, &Category{
			Title:  m.Title,
			PageID: m.PageID,
			NS:     m.NS,
		})
	}
	return out, nil
}

// RecentUploads returns recently uploaded files.
func (c *Client) RecentUploads(ctx context.Context, limit int) ([]*File, error) {
	if limit <= 0 {
		limit = 20
	}
	params := url.Values{
		"action":      {"query"},
		"list":        {"recentchanges"},
		"rcnamespace": {"6"},
		"rclimit":     {fmt.Sprintf("%d", limit)},
		"rctype":      {"new"},
		"format":      {"json"},
	}
	rawURL := c.BaseURL + "?" + params.Encode()
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	var wire wireRecentResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("recent decode: %w", err)
	}
	out := make([]*File, 0, len(wire.Query.RecentChanges))
	for _, rc := range wire.Query.RecentChanges {
		out = append(out, &File{
			FileName: fileBaseName(rc.Title),
			Title:    rc.Title,
			PageID:   rc.PageID,
			Date:     rc.Timestamp,
		})
	}
	return out, nil
}

// ---------- helpers ----------

// fileBaseName strips the "File:" prefix from a wiki title, returning just the
// bare filename used as the kit id and as input to GetFile.
func fileBaseName(title string) string {
	return strings.TrimPrefix(title, "File:")
}

// stripHTML removes simple HTML tags from a string (description/author fields
// from extmetadata often contain markup).
var htmlTagReplacer = strings.NewReplacer("<", " <", ">", "> ")

func stripHTML(s string) string {
	// Remove tags by iterating over < > pairs.
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	// Collapse whitespace.
	return strings.Join(strings.Fields(b.String()), " ")
}
