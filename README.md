# commons

A command line for commons.

`commons` is a single pure-Go binary. It reads public commons data
over plain HTTPS, shapes it into clean records, and prints output that pipes
into the rest of your tools. No API key, nothing to run alongside it.

The same package is also a [resource-URI driver](#use-it-as-a-resource-uri-driver),
so a host program like [ant](https://github.com/tamnd/ant) can address
commons as `commons://` URIs.

## Install

```bash
go install github.com/tamnd/commons-cli/cmd/commons@latest
```

Or grab a prebuilt binary from the [releases](https://github.com/tamnd/commons-cli/releases), or run
the container image:

```bash
docker run --rm ghcr.io/tamnd/commons:latest --help
```

## Usage

```bash
commons page <path>                      # fetch one page as a record
commons page <path> -o json              # as JSON, ready for jq
commons page <path> --template '{{.Body}}'  # just the readable body text
commons links <path>                     # the pages it links to, one per line
commons --help                           # the whole command tree
```

Every command shares one output contract: `-o table|json|jsonl|csv|tsv|url|raw`,
`--fields` to pick columns, `--template` for a custom line, and `-n` to limit.
The default adapts to where output goes (a table on a terminal, JSONL in a
pipe), so the same command reads well by hand and parses cleanly downstream.

This is a fresh scaffold. It ships one example resource type, `page`, wired end
to end. Model the real commons records in `commons/` and declare their
operations in `commons/domain.go`; each one becomes a command, an HTTP
route, and an MCP tool at once.

## Serve it

The same operations are available over HTTP and as an MCP tool set for agents,
with no extra code:

```bash
commons serve --addr :7777    # GET /v1/page/<path>  returns NDJSON
commons mcp                   # speak MCP over stdio
```

## Use it as a resource-URI driver

`commons` registers a `commons` domain the way a program registers a
database driver with `database/sql`. A host enables it with one blank import:

```go
import _ "github.com/tamnd/commons-cli/commons"
```

Then [ant](https://github.com/tamnd/ant) (or any program that links the package)
dereferences `commons://` URIs without knowing anything about commons:

```bash
ant get commons://page/<path>   # fetch the record
ant cat commons://page/<path>   # just the body text
ant ls  commons://page/<path>   # the pages it links to, each addressable
ant url commons://page/<path>   # the live https URL
```

## Development

```
cmd/commons/   thin main: hands cli.NewApp to kit.Run
cli/                 assembles the kit App from the commons domain
commons/                the library: HTTP client, data models, and domain.go (the driver)
docs/                tago documentation site
```

```bash
make build      # ./bin/commons
make test       # go test ./...
make vet        # go vet ./...
```

## Releasing

Push a version tag and GitHub Actions runs GoReleaser, which builds the
archives, Linux packages, the multi-arch GHCR image, checksums, SBOMs, and a
cosign signature:

```bash
git tag v0.1.0
git push --tags
```

The Homebrew and Scoop steps self-disable until their tokens exist, so the first
release works with no extra secrets.

## License

Apache-2.0. See [LICENSE](LICENSE).
