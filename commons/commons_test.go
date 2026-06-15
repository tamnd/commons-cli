package commons

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestClient returns a Client pointed at srv with pacing disabled.
func newTestClient(srv *httptest.Server) *Client {
	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	c := NewClient(cfg)
	return c
}

func TestGetSetsUserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte(`{"query":{"searchinfo":{"totalhits":0},"search":[]}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.SearchFiles(context.Background(), "cat", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"query":{"searchinfo":{"totalhits":0},"search":[]}}`))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 5
	c := NewClient(cfg)

	start := time.Now()
	_, err := c.SearchFiles(context.Background(), "cat", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestSearchFiles(t *testing.T) {
	resp := wireSearchResponse{}
	resp.Query.SearchInfo.TotalHits = 2
	resp.Query.Search = []struct {
		Title  string `json:"title"`
		PageID int    `json:"pageid"`
	}{
		{Title: "File:Cat03.jpg", PageID: 12345},
		{Title: "File:Cat04.jpg", PageID: 12346},
	}
	raw, _ := json.Marshal(resp)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("action") != "query" || q.Get("list") != "search" {
			t.Errorf("unexpected query params: %v", q)
		}
		if q.Get("srnamespace") != "6" {
			t.Errorf("srnamespace = %q, want 6", q.Get("srnamespace"))
		}
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	files, err := c.SearchFiles(context.Background(), "cat", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("len = %d, want 2", len(files))
	}
	if files[0].Title != "File:Cat03.jpg" || files[0].PageID != 12345 {
		t.Errorf("files[0] = %+v", files[0])
	}
}

func TestGetFile(t *testing.T) {
	// Build a minimal wire response.
	raw := `{
		"query":{
			"pages":{
				"12345":{
					"title":"File:Cat03.jpg",
					"pageid":12345,
					"imageinfo":[{
						"url":"https://upload.wikimedia.org/wikipedia/commons/4/4d/Cat03.jpg",
						"size":279603,
						"mediatype":"BITMAP",
						"extmetadata":{
							"ImageDescription":{"value":"A domestic cat"},
							"Artist":{"value":"Contributor"},
							"LicenseShortName":{"value":"CC BY-SA 3.0"},
							"DateTime":{"value":"2012-03-15"}
						}
					}]
				}
			}
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("action") != "query" || q.Get("prop") != "imageinfo" {
			t.Errorf("unexpected query params: %v", q)
		}
		// The title param must contain "File:"
		if title := q.Get("titles"); title == "" {
			t.Error("titles param missing")
		}
		_, _ = w.Write([]byte(raw))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	f, err := c.GetFile(context.Background(), "Cat03.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "File:Cat03.jpg" {
		t.Errorf("Title = %q, want File:Cat03.jpg", f.Title)
	}
	if f.Size != 279603 {
		t.Errorf("Size = %d, want 279603", f.Size)
	}
	if f.MediaType != "BITMAP" {
		t.Errorf("MediaType = %q, want BITMAP", f.MediaType)
	}
	if f.License != "CC BY-SA 3.0" {
		t.Errorf("License = %q, want CC BY-SA 3.0", f.License)
	}
	if f.Description != "A domestic cat" {
		t.Errorf("Description = %q, want A domestic cat", f.Description)
	}
}

func TestGetFileAddsPrefix(t *testing.T) {
	raw := `{"query":{"pages":{"12345":{"title":"File:Cat03.jpg","pageid":12345,"imageinfo":[{"url":"https://example.com/cat.jpg","size":100,"mediatype":"BITMAP","extmetadata":{}}]}}}}`
	var gotTitle string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTitle = r.URL.Query().Get("titles")
		_, _ = w.Write([]byte(raw))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.GetFile(context.Background(), "Cat03.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if gotTitle != "File:Cat03.jpg" {
		t.Errorf("titles param = %q, want File:Cat03.jpg", gotTitle)
	}
}

func TestListCategory(t *testing.T) {
	raw := `{
		"query":{
			"categorymembers":[
				{"title":"File:Cat03.jpg","pageid":12345,"ns":6},
				{"title":"File:Cat04.jpg","pageid":12346,"ns":6}
			]
		}
	}`
	var gotCmTitle string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCmTitle = r.URL.Query().Get("cmtitle")
		_, _ = w.Write([]byte(raw))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	cats, err := c.ListCategory(context.Background(), "Cats", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 2 {
		t.Fatalf("len = %d, want 2", len(cats))
	}
	if cats[0].Title != "File:Cat03.jpg" || cats[0].PageID != 12345 || cats[0].NS != 6 {
		t.Errorf("cats[0] = %+v", cats[0])
	}
	if gotCmTitle != "Category:Cats" {
		t.Errorf("cmtitle = %q, want Category:Cats", gotCmTitle)
	}
}

func TestListCategoryAddsPrefix(t *testing.T) {
	raw := `{"query":{"categorymembers":[]}}`
	var gotCmTitle string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCmTitle = r.URL.Query().Get("cmtitle")
		_, _ = w.Write([]byte(raw))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.ListCategory(context.Background(), "Cats", 5)
	if err != nil {
		t.Fatal(err)
	}
	if gotCmTitle != "Category:Cats" {
		t.Errorf("cmtitle = %q, want Category:Cats", gotCmTitle)
	}

	// When "Category:" prefix already present, must not double it.
	_, err = c.ListCategory(context.Background(), "Category:Cats", 5)
	if err != nil {
		t.Fatal(err)
	}
	if gotCmTitle != "Category:Cats" {
		t.Errorf("cmtitle with prefix = %q, want Category:Cats", gotCmTitle)
	}
}

func TestRecentUploads(t *testing.T) {
	raw := `{
		"query":{
			"recentchanges":[
				{"title":"File:New1.jpg","pageid":1001,"timestamp":"2024-01-01T00:00:00Z"},
				{"title":"File:New2.jpg","pageid":1002,"timestamp":"2024-01-01T00:01:00Z"}
			]
		}
	}`
	var gotAction, gotList string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAction = r.URL.Query().Get("action")
		gotList = r.URL.Query().Get("list")
		_, _ = w.Write([]byte(raw))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	files, err := c.RecentUploads(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("len = %d, want 2", len(files))
	}
	if files[0].Title != "File:New1.jpg" || files[0].PageID != 1001 {
		t.Errorf("files[0] = %+v", files[0])
	}
	if gotAction != "query" || gotList != "recentchanges" {
		t.Errorf("action=%q list=%q", gotAction, gotList)
	}
}

func TestStripHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain text", "plain text"},
		{"<b>bold</b> text", "bold text"},
		{"<a href=\"x\">link</a>", "link"},
		{"  spaces  ", "spaces"},
	}
	for _, tc := range cases {
		if got := stripHTML(tc.in); got != tc.want {
			t.Errorf("stripHTML(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBackoff(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 500 * time.Millisecond},
		{2, 1000 * time.Millisecond},
		{10, 5 * time.Second}, // capped
	}
	for _, tc := range cases {
		got := backoff(tc.attempt)
		if got != tc.want {
			t.Errorf("backoff(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}
