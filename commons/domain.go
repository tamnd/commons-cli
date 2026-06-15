package commons

import (
	"context"
	"net/url"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes Wikimedia Commons as a kit Domain: a driver that a
// multi-domain host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/commons-cli/commons"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then
// dereferences commons:// URIs by routing to the operations Register
// installs. The same Domain also builds the standalone commons binary (see
// cli/root.go), so the binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the Wikimedia Commons driver. It carries no state; the
// per-run client is built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against,
// and the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme:  "commons",
		Aliases: []string{"wmc"},
		Hosts:   []string{Host},
		Identity: kit.Identity{
			Binary: "commons",
			Short:  "Browse 143M Wikimedia Commons media files",
			Long: `commons reads public Wikimedia Commons data over plain HTTPS, shapes
it into clean records, and prints output that pipes into the rest of your
tools. No API key, nothing to run alongside it.`,
			Site: Host,
			Repo: "https://github.com/tamnd/commons-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	// search: keyword search across all files
	kit.Handle(app, kit.OpMeta{
		Name:    "search",
		Group:   "files",
		Summary: "Search files by keyword",
		Args:    []kit.Arg{{Name: "query", Help: "search terms"}},
	}, searchFiles)

	// file: single file info + metadata
	kit.Handle(app, kit.OpMeta{
		Name:     "file",
		Group:    "files",
		Single:   true,
		Summary:  "Get file info and metadata",
		URIType:  "file",
		Resolver: true,
		Args:     []kit.Arg{{Name: "title", Help: "filename (without File: prefix)"}},
	}, getFile)

	// category: list files in a category
	kit.Handle(app, kit.OpMeta{
		Name:    "category",
		Group:   "files",
		List:    true,
		Summary: "List files in a category",
		URIType: "category",
		Args:    []kit.Arg{{Name: "name", Help: "category name (without Category: prefix)"}},
	}, listCategory)

	// recent: recent uploads
	kit.Handle(app, kit.OpMeta{
		Name:    "recent",
		Group:   "files",
		Summary: "List recent file uploads",
	}, recentUploads)
}

// newClient builds the Wikimedia Commons client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	ccfg := DefaultConfig()
	if cfg.UserAgent != "" {
		ccfg.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		ccfg.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		ccfg.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		ccfg.Timeout = cfg.Timeout
	}
	return NewClient(ccfg), nil
}

// ---------- inputs ----------

type searchInput struct {
	Query  string  `kit:"arg" help:"search terms"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Start  int     `kit:"flag" help:"result offset for pagination"`
	Client *Client `kit:"inject"`
}

type fileInput struct {
	Title  string  `kit:"arg" help:"filename (without File: prefix)"`
	Client *Client `kit:"inject"`
}

type categoryInput struct {
	Name   string  `kit:"arg" help:"category name (without Category: prefix)"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

type recentInput struct {
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

// ---------- handlers ----------

func searchFiles(ctx context.Context, in searchInput, emit func(*File) error) error {
	files, err := in.Client.SearchFiles(ctx, in.Query, in.Limit, in.Start)
	if err != nil {
		return mapErr(err)
	}
	for _, f := range files {
		if err := emit(f); err != nil {
			return err
		}
	}
	return nil
}

func getFile(ctx context.Context, in fileInput, emit func(*File) error) error {
	f, err := in.Client.GetFile(ctx, in.Title)
	if err != nil {
		return mapErr(err)
	}
	return emit(f)
}

func listCategory(ctx context.Context, in categoryInput, emit func(*Category) error) error {
	cats, err := in.Client.ListCategory(ctx, in.Name, in.Limit)
	if err != nil {
		return mapErr(err)
	}
	for _, c := range cats {
		if err := emit(c); err != nil {
			return err
		}
	}
	return nil
}

func recentUploads(ctx context.Context, in recentInput, emit func(*File) error) error {
	files, err := in.Client.RecentUploads(ctx, in.Limit)
	if err != nil {
		return mapErr(err)
	}
	for _, f := range files {
		if err := emit(f); err != nil {
			return err
		}
	}
	return nil
}

// ---------- Resolver ----------

// Classify turns any accepted input into the canonical (uriType, id).
// For file URLs and "File:" prefixed titles the type is "file".
// For category URLs the type is "category".
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)

	// Full URL on commons.wikimedia.org
	if u, perr := url.Parse(input); perr == nil && (u.Scheme == "http" || u.Scheme == "https") {
		path := strings.Trim(u.Path, "/")
		// /wiki/File:Foo.jpg  or  /wiki/Category:Foo
		if rest, ok := strings.CutPrefix(path, "wiki/"); ok {
			return classifyWikiTitle(rest)
		}
		return "", "", errs.Usage("unrecognized commons URL: %q", input)
	}

	return classifyWikiTitle(input)
}

// classifyWikiTitle classifies a bare wiki title or prefixed name.
func classifyWikiTitle(s string) (uriType, id string, err error) {
	switch {
	case strings.HasPrefix(s, "File:"):
		return "file", strings.TrimPrefix(s, "File:"), nil
	case strings.HasPrefix(s, "Category:"):
		return "category", strings.TrimPrefix(s, "Category:"), nil
	default:
		// Treat bare names as file titles (most common use case).
		if s != "" {
			return "file", s, nil
		}
		return "", "", errs.Usage("unrecognized commons reference: %q", s)
	}
}

// Locate is the inverse: the live https URL for a (uriType, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "file":
		title := "File:" + id
		return BaseURL + "/wiki/" + url.PathEscape(strings.ReplaceAll(title, " ", "_")), nil
	case "category":
		title := "Category:" + id
		return BaseURL + "/wiki/" + url.PathEscape(strings.ReplaceAll(title, " ", "_")), nil
	default:
		return "", errs.Usage("commons has no resource type %q", uriType)
	}
}

// ---------- helpers ----------

func mapErr(err error) error {
	return err
}
