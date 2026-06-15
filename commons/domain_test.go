package commons

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring (mint, body, resolve), which need no network. The
// client's HTTP behaviour is covered in commons_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "commons" {
		t.Errorf("Scheme = %q, want commons", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "commons" {
		t.Errorf("Identity.Binary = %q, want commons", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		in, wantType, wantID string
	}{
		// bare filename without prefix → file
		{"Cat03.jpg", "file", "Cat03.jpg"},
		// File: prefix → file
		{"File:Cat03.jpg", "file", "Cat03.jpg"},
		// Category: prefix → category
		{"Category:Cats", "category", "Cats"},
		// Full URL with File:
		{"https://commons.wikimedia.org/wiki/File:Cat03.jpg", "file", "Cat03.jpg"},
		// Full URL with Category:
		{"https://commons.wikimedia.org/wiki/Category:Cats", "category", "Cats"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.wantType || id != tc.wantID {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.wantType, tc.wantID)
		}
	}
}

func TestLocate(t *testing.T) {
	cases := []struct {
		uriType, id, want string
	}{
		{"file", "Cat03.jpg", "https://commons.wikimedia.org/wiki/File:Cat03.jpg"},
		{"category", "Cats", "https://commons.wikimedia.org/wiki/Category:Cats"},
		{"file", "Space photo.jpg", "https://commons.wikimedia.org/wiki/File:Space_photo.jpg"},
	}
	for _, tc := range cases {
		got, err := Domain{}.Locate(tc.uriType, tc.id)
		if err != nil || got != tc.want {
			t.Errorf("Locate(%q, %q) = (%q, %v), want (%q, nil)",
				tc.uriType, tc.id, got, err, tc.want)
		}
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("bogus", "foo")
	if err == nil {
		t.Error("expected error for unknown type, got nil")
	}
}

func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	f := &File{
		FileName: "Cat03.jpg",
		Title:    "File:Cat03.jpg",
		PageID:   12345,
		URL:      "https://upload.wikimedia.org/wikipedia/commons/4/4d/Cat03.jpg",
	}
	u, err := h.Mint(f)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if u.Scheme != "commons" {
		t.Errorf("Mint scheme = %q, want commons", u.Scheme)
	}
	if want := "commons://file/Cat03.jpg"; u.String() != want {
		t.Errorf("Mint = %q, want %q", u.String(), want)
	}

	got, err := h.ResolveOn("commons", "Cat03.jpg")
	if err != nil {
		t.Fatalf("ResolveOn: %v", err)
	}
	if got.Scheme != "commons" {
		t.Errorf("ResolveOn scheme = %q, want commons", got.Scheme)
	}
}
