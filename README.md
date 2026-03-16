# unlog

[![CI](https://github.com/oxforge/unlog/actions/workflows/ci.yml/badge.svg)](https://github.com/oxforge/unlog/actions/workflows/ci.yml)
[![Release](https://github.com/oxforge/unlog/actions/workflows/release.yml/badge.svg)](https://github.com/oxforge/unlog/actions/workflows/release.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/oxforge/unlog)](https://goreportcard.com/report/github.com/oxforge/unlog)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Unravel your logs.** A fast CLI tool that ingests raw log files, extracts signal from noise at disk speed, and optionally uses LLMs to produce incident timelines and root cause analysis.

unlog processes logs through a 6-stage streaming pipeline via Go channels with back-pressure:

```
Files/stdin → [Ingest] → [Filter] → [Enrich] → [Compact] → [Analyze*] → [Render]
                        (N workers)                       (* optional)
```

Stages 1-4 are pure Go with zero network calls, making the core useful without any AI provider. Stage 5 adds optional LLM analysis via OpenAI, Anthropic, or Ollama.

```
cat /var/log/app/*.log | unlog
unlog --ai-provider openai logs/
```

## Features

- **10 log format parsers** -- JSON, logfmt, syslog (RFC 3164/5424), Apache CLF, Docker JSON, Kubernetes, CloudWatch, generic timestamped, and raw fallback. Format auto-detected via majority vote.
- **Compressed file support** -- Reads `.gz`, `.tar.gz`, and `.tgz` files transparently.
- **Noise removal** -- Built-in noise patterns (health checks, Prometheus scrapes, k8s internals) plus custom pattern files.
- **Fuzzy deduplication** -- Normalizes UUIDs, IPs, timestamps, and paths to group similar messages. Sharded LRU cache for concurrent access.
- **Rate spike detection** -- Per-source sliding window detects anomalous event rates.
- **Error chain detection** -- 10 built-in patterns (DB exhaustion, OOM cascade, circuit breaker, disk full, cert expiry, DNS failure, etc.).
- **Token-budgeted compaction** -- Priority-scored entries fit within LLM context windows.
- **Multi-pass LLM analysis** -- Timeline, root cause, and recommendations in three chained passes. Single-pass fast mode available.
- **Streaming output** -- LLM responses stream to the terminal token-by-token.
- **Multiple output formats** -- Colored terminal, JSON, and Markdown.
- **Stdin support** -- Pipe logs from any source.
- **Importable as a library** -- `types/`, `ingest/`, `filter/`, `enrich/`, and `compact/` are public packages.

## Installation

### Homebrew

```bash
brew install oxforge/tap/unlog
```

### Shell script

```bash
curl -fsSL https://raw.githubusercontent.com/oxforge/unlog/main/install.sh | sh
```

### From source

Requires Go 1.22+.

```bash
go install github.com/oxforge/unlog@latest
```

### Build from repo

```bash
git clone https://github.com/oxforge/unlog.git
cd unlog
go build -o bin/unlog .   # binary at ./bin/unlog
go install .              # install to $GOPATH/bin
```

## Quick start

### Without AI (stages 1-4 only)

Analyze a directory of log files and print a structured summary:

```bash
unlog /var/log/app/
```

Pipe from another command:

```bash
kubectl logs deployment/api --since=1h | unlog
```

Get filter statistics without the full summary:

```bash
unlog stats /var/log/app/
```

Detect log formats in a directory:

```bash
unlog formats /var/log/
```

### With AI

Set your API key and run:

```bash
export OPENAI_API_KEY=sk-...
unlog --ai-provider openai /var/log/app/
```

Use a different provider:

```bash
export ANTHROPIC_API_KEY=sk-ant-...
unlog --ai-provider anthropic logs/

# Local models via Ollama (no API key needed)
unlog --ai-provider ollama --model llama3 logs/
```

Fast mode (single LLM pass, quicker but less detailed):

```bash
unlog --ai-provider openai --fast logs/
```

### Output formats

```bash
# Colored terminal output (default)
unlog logs/

# JSON for CI/CD pipelines
unlog --format json logs/

# Markdown for reports
unlog --ai-provider openai --format markdown --output report.md logs/
```

## Commands

| Command | Description |
|---------|-------------|
| `analyze` | Run the full pipeline and produce an incident summary (**default** -- runs when no subcommand is given) |
| `stats` | Show filter statistics (stages 1-2 only, no AI) |
| `formats` | Detect and display log formats found in input files |
| `version` | Print the version |

## Flags

### Global flags

| Flag | Description |
|------|-------------|
| `--verbose`, `-v` | Show detailed output including per-filter drop counts |
| `--no-color` | Disable colored output (also respects `NO_COLOR` env var) |
| `--config` | Config file path (default: `~/.unlog/config.toml`) |

### Analyze flags (available on root and `analyze` subcommand)

| Flag | Description |
|------|-------------|
| `--ai-provider` | Enable LLM analysis with provider: `openai`, `anthropic`, `ollama` |
| `--model` | Override the default model for the chosen provider |
| `--fast` | Single-pass LLM analysis (faster, less detailed) |
| `--format` | Output format: `text`, `json`, `markdown` |
| `--output` | Write output to a file instead of stdout |
| `--level` | Minimum log level: `trace`, `debug`, `info`, `warn`, `error`, `fatal` (default: `warn`) |
| `--since` | Start time filter (ISO 8601 or relative: `2h`, `30m`) |
| `--until` | End time filter (ISO 8601 or relative: `2h`, `30m`) |
| `--noise-file` | Path to a custom noise patterns file |

## Configuration

Settings are resolved in priority order: **CLI flags > environment variables > config file > defaults**.

### Config file

Create `~/.unlog/config.toml`:

```toml
level = "warn"
format = "text"
ai_provider = ""       # set to "openai", "anthropic", or "ollama" to enable AI
model = ""
noise_file = ""
verbose = false
no_color = false
```

### Environment variables

Every config option can be set via `UNLOG_*` environment variables:

| Variable | Description |
|----------|-------------|
| `UNLOG_LEVEL` | Minimum log level |
| `UNLOG_FORMAT` | Output format |
| `UNLOG_AI_PROVIDER` | LLM provider (empty = no AI) |
| `UNLOG_MODEL` | Model override |
| `UNLOG_NOISE_FILE` | Custom noise patterns path |
| `UNLOG_VERBOSE` | Verbose output (`true`/`false`) |
| `NO_COLOR` | Disable color (any value, per [no-color.org](https://no-color.org)) |

### LLM provider API keys

| Provider | Environment variable |
|----------|---------------------|
| OpenAI | `OPENAI_API_KEY` |
| Anthropic | `ANTHROPIC_API_KEY` |
| Ollama | None required (connects to `localhost:11434`) |

## Supported log formats

unlog auto-detects the format of each input file by sampling lines and using majority vote. No configuration needed.

| Format | Example |
|--------|---------|
| **JSON** | `{"level":"error","msg":"timeout","ts":"2024-01-15T10:00:00Z"}` |
| **logfmt** | `ts=2024-01-15T10:00:00Z level=error msg="timeout"` |
| **Syslog RFC 3164** | `<131>Jan 15 10:00:00 app-1 myapp[1234]: ERROR timeout` |
| **Syslog RFC 5424** | `<165>1 2024-01-15T10:00:00Z host app 1234 - - timeout` |
| **Apache CLF** | `10.0.0.1 - - [15/Jan/2024:10:00:00 -0700] "GET /api HTTP/1.1" 500 789` |
| **Docker JSON** | `{"log":"timeout\n","stream":"stderr","time":"2024-01-15T..."}` |
| **Kubernetes** | `2024-01-15T10:00:00.000Z stderr F timeout` |
| **CloudWatch** | `{"@timestamp":"2024-01-15T10:00:00Z","@message":"timeout"}` |
| **Generic** | `2024-01-15 10:00:00 ERROR timeout` |
| **Raw** | Any unstructured text (fallback) |

### Compressed files

| Extension | Handling |
|-----------|----------|
| `.gz` | Decompressed, inner file processed as a single source |
| `.tar.gz`, `.tgz` | Each log file in the archive processed separately |

## Architecture

unlog uses a 6-stage streaming pipeline connected by buffered Go channels:

```
Files/stdin --> [Ingest] --> [Filter] --> [Enrich] --> [Compact] --> [Analyze*] --> [Render]
                              N workers                              * optional
```

| Stage | Package | Description |
|-------|---------|-------------|
| 1. Ingest | `ingest/` | Read files, detect format, parse into structured entries |
| 2. Filter | `filter/` | Level filter, time window, noise removal, dedup, spike detection |
| 3. Enrich | `enrich/` | Error chain detection, deployment events, field extraction |
| 4. Compact | `compact/` | Priority scoring, token-budgeted compaction |
| 5. Analyze | `internal/analyze/` | Optional LLM multi-pass analysis |
| 6. Render | `internal/render/` | Terminal, JSON, and Markdown output |

Stages 1-4 are pure Go with no network calls. Stage 5 is optional (enabled with `--ai-provider`). Stages 1-4 are public packages importable as a Go library.

## Using as a library

The public packages (`types/`, `ingest/`, `filter/`, `enrich/`, `compact/`) can be imported directly:

```go
package main

import (
    "context"
    "fmt"

    "github.com/oxforge/unlog/compact"
    "github.com/oxforge/unlog/enrich"
    "github.com/oxforge/unlog/filter"
    "github.com/oxforge/unlog/ingest"
    "github.com/oxforge/unlog/types"
)

func main() {
    ctx := context.Background()

    ingestCh := make(chan types.LogEntry, 100_000)
    filterCh := make(chan types.FilteredEntry, 100_000)
    enrichCh := make(chan types.EnrichedEntry, 100_000)

    // Stage 1: Ingest
    go func() {
        ing := ingest.NewIngester(ingestCh, ingest.IngestOptions{})
        ing.Run(ctx, []string{"/var/log/app/"})
    }()

    // Stage 2: Filter
    go func() {
        fp := filter.NewFilterPipeline(ingestCh, filterCh, filter.DefaultFilterOptions(), nil)
        fp.Run(ctx)
    }()

    // Stage 3: Enrich
    go func() {
        ep := enrich.NewEnricher(filterCh, enrichCh, enrich.DefaultOptions())
        ep.Run(ctx)
    }()

    // Stage 4: Compact
    summary, _ := compact.Run(ctx, enrichCh, compact.Options{TokenBudget: 4096})
    fmt.Println(summary)
}
```

## Custom noise patterns

Create a text file with one pattern per line (case-insensitive substring match). Lines starting with `#` are comments.

```text
# My custom noise patterns
GET /internal/status
connection pool stats
scheduled task completed
cache warmup
```

Use it with `--noise-file`:

```bash
unlog --noise-file my-patterns.txt logs/
```

The built-in patterns cover health checks, Prometheus scrapes, Kubernetes internals, TLS handshakes, connection pool stats, and AWS SDK retries.

## JSON output schema

When using `--format json`, the output includes:

```json
{
  "generated_at": "2024-01-15T12:00:00Z",
  "unlog_version": "1.0.0",
  "analysis_duration": 1234567,
  "stats": {
    "total_ingested": 50000,
    "total_dropped": 49500,
    "total_survived": 500,
    "unique_signatures": 42,
    "time_window_start": "2024-01-15T10:00:00Z",
    "time_window_end": "2024-01-15T10:05:00Z",
    "source_breakdown": { "api.log": 30, "db.log": 12 }
  },
  "compacted_summary": "## Incident Overview\n..."
}
```

When AI analysis is included, the output adds `timeline`, `root_cause`, and `recommendations` fields.

## Development

```bash
go build -o bin/unlog .                              # Build binary
go test ./... -race                                  # Run all tests with race detector
golangci-lint run                                    # Lint
go test ./internal/... -bench=. -benchmem            # Run benchmarks
go run testdata/big/generate.go -h                   # Generate synthetic test data
go install .                                         # Install to $GOPATH/bin
```

### Running tests

```bash
go test ./... -race                    # Full suite
go test ./ingest/ -run TestIngester    # Specific package/test
go test ./filter/ -bench=. -benchmem   # Benchmarks for one package
```

### Generating test data

Use `testdata/big/generate.go` to create synthetic log files for benchmarking and manual testing. See its [README](testdata/big/README.md) for usage, flags, and examples.

### Project structure

```
cmd/                    CLI commands (Cobra)
types/                  Shared types (LogEntry, Level, FilterStats)
ingest/                 Stage 1: file reading, format detection, parsing
filter/                 Stage 2: level filter, dedup, noise, spike detection
enrich/                 Stage 3: error chains, deployment events, correlation
compact/                Stage 4: priority scoring, token budgeting
internal/
  analyze/              Stage 5: LLM providers (OpenAI, Anthropic, Ollama)
  render/               Stage 6: terminal, JSON, markdown renderers
  pipeline/             Channel wiring and orchestration
  config/               TOML config loading with env overlay
noise/                  Embedded default noise patterns
testdata/               Test fixtures (log samples, incident scenarios)
```

## Requirements

- Go 1.22+ (build)
- No CGO required
- No runtime dependencies beyond the Go standard library and three small modules:
  - `github.com/spf13/cobra` -- CLI framework
  - `github.com/BurntSushi/toml` -- config file parsing
  - `golang.org/x/sync` -- errgroup for goroutine management

## License

MIT
