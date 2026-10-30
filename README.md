# unlog

[![CI](https://github.com/oxforge/unlog/actions/workflows/ci.yml/badge.svg)](https://github.com/oxforge/unlog/actions/workflows/ci.yml)
[![Release](https://github.com/oxforge/unlog/actions/workflows/release.yml/badge.svg)](https://github.com/oxforge/unlog/actions/workflows/release.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/oxforge/unlog)](https://goreportcard.com/report/github.com/oxforge/unlog)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Unravel your logs.** A fast CLI tool that reads raw log files at disk speed and turns them into a short incident summary: what failed, where, how often, in what order, and what was deployed just before. People read it directly; AI agents read it instead of grepping through the logs (see [For agents](#for-agents)).

unlog processes logs through a 5-stage streaming pipeline via Go channels with back-pressure:

![Pipeline](docs/pipeline.png)

unlog makes no network calls and never needs an API key.

```
cat /var/log/app/*.log | unlog
unlog logs/
```

## Features

- **12 log format parsers** -- JSON, logfmt, CSV, syslog (RFC 3164/5424 and rsyslog's file formats), Apache CLF, Docker JSON, Docker Compose, Kubernetes, CloudWatch, generic timestamped, and raw fallback. Format auto-detected via majority vote.
- **Compressed file support** -- Reads `.gz`, `.tar.gz`, and `.tgz` files transparently.
- **Noise removal** -- Built-in noise patterns (health checks, Prometheus scrapes, k8s internals) plus custom pattern files.
- **One line per error** -- Normalizes UUIDs, IPs, timestamps, and paths to group similar messages; each distinct error per source (and service) becomes one line with when it started and stopped and how many times it occurred. The same input always gives the same summary.
- **Services within a file** -- When lines name a service, app, container or pod, errors and bursts are reported per service (`app.log[payments]`); replicas share their service (Compose `web-1` and `web-2` are `web`, pod `api-7d9f8b6c4-x2k4p` is `api`). Errors that started within seconds of each other in different sources or services are tagged as failing together (`with=`).
- **Burst detection** -- Finds stretches where a source or service logged far above its normal rate, counted on real event volume, and lists the errors behind each burst.
- **Error chain detection** -- 10 built-in patterns (DB exhaustion, OOM cascade, circuit breaker, deployment failure, disk full, cert expiry, DNS failure, etc.); a chain is reported when at least two of its steps occur in order (any two: a log that skips a step, or starts after the first one, still counts), with its errors in order; an error can belong to more than one chain.
- **Deployment markers** -- Deploy, restart, scaling and rollback lines are kept at any `--level` and listed before the errors, each version on its own line (they merge only when identical), with how long before the first error the last deploy happened.
- **Token-budgeted compaction** -- Priority-scored entries fit a fixed size, small enough for an AI agent's context window whatever the size of the input.
- **Agent plugin** -- A Claude Code plugin whose skill makes agents run unlog first when they analyze logs, then answer with the facts and a conclusion only where the evidence supports one (see [For agents](#for-agents)).
- **Multiple output formats** -- Colored terminal, JSON, and Markdown.
- **Stdin support** -- Pipe logs from any source.
- **Importable as a library** -- `types/`, `ingest/`, `filter/`, `enrich/`, and `compact/` are public packages.

## Installation

On macOS or Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/oxforge/unlog/main/install.sh | sh
```

The script downloads the latest release for your system, checks it against the
release's `checksums.txt` and puts `unlog` in `~/.local/bin`; `UNLOG_INSTALL_DIR`
changes the folder. To install a specific version, give the script that version
(`curl -fsSL https://raw.githubusercontent.com/oxforge/unlog/main/install.sh | UNLOG_VERSION=v0.2.2 sh`).
Release archives for every platform, Windows included, are on the
[releases page](https://github.com/oxforge/unlog/releases).

Or, with Go 1.27+ (on any platform):

```bash
go install github.com/oxforge/unlog@latest
```

## Quick start

Analyze a directory of log files and print a structured summary:

```bash
unlog /var/log/app/
```

Directories are searched recursively for log files, including rotated ones (`app.log.1`, `app.log-20240115`), compressed ones (`app.log.3.gz`) and `.tar.gz` archives; hidden files, binary files (like `wtmp`) and unreadable files are skipped (`-v` says which and why). A directory given as a symlink (`current/`) is searched as usual, and a symlink inside one to a directory elsewhere is followed once; a file reached by two paths is read once. A log truncated in place (logrotate's `copytruncate`) is read past its leading NUL bytes. A path that doesn't exist, a directory or archive from which nothing could be read, or a file named on the command line that can't be read or isn't a text log, is an error (exit code 1). To have unlog skip such files, give it their directory (`unlog /var/log`) or a quoted glob it expands itself (`unlog '/var/log/*.log'`) rather than a shell glob. The `--output` file is never read as input. `-` reads stdin and can be combined with files. `unlog analyze <paths>` is the same as `unlog <paths>`.

Pipe from another command:

```bash
kubectl logs deployment/api --since=1h | unlog
```

### With an AI agent

unlog itself calls no LLM. Hand its summary to the agent you already use, or install the plugin from [For agents](#for-agents) so the agent runs unlog on its own whenever it analyzes logs:

```bash
unlog logs/ | claude -p "What went wrong in these logs?"
```

### Output formats

```bash
# Colored terminal output (default)
unlog logs/

# JSON for CI/CD pipelines
unlog --format json logs/

# Markdown for reports
unlog --format markdown --output report.md logs/
```

## For agents

This repository is also a Claude Code plugin. Its `analyzing-logs` skill makes an agent that is asked to analyze logs run unlog on them first, instead of grepping them or writing a script to read them. The agent then reads the summary, looks at the raw logs only where the summary raises a question, and answers with the facts, adding a conclusion only when the order of events supports one. A SessionStart hook puts that rule in every session.

```bash
claude plugin marketplace add oxforge/unlog
claude plugin install unlog@unlog
```

On Claude Code 2.1.275 or later, `/plugin install unlog --marketplace oxforge/unlog` does both in one step. The plugin follows unlog's releases and needs the `unlog` binary itself ([Installation](#installation)); when the binary is missing, the agent says so and asks before installing it.

Any other agent with a shell (Codex, Cursor, Gemini CLI and others) can use the same skill: add [`skills/analyzing-logs/SKILL.md`](skills/analyzing-logs/SKILL.md) and its reference, [`reading-the-summary.md`](skills/analyzing-logs/reading-the-summary.md), to the agent's instructions or skills folder. Neither file relies on anything specific to Claude Code.

## Flags

| Flag | Description |
|------|-------------|
| `--format` | Output format: `text`, `json`, `markdown` |
| `--output` | Write output to a file instead of stdout |
| `--level` | Minimum log level: `trace`, `debug`, `info`, `warn`, `error`, `fatal` (default: `warn`) |
| `--since` | Start time filter (ISO 8601 or relative: `2h`, `30m`) |
| `--until` | End time filter (ISO 8601 or relative: `2h`, `30m`) |
| `--tz` | Time zone of timestamps that carry none, in logs and in `--since`/`--until` (e.g. `Europe/Berlin`, `Local`; default: UTC) |
| `--noise-file` | Path to a custom noise patterns file |
| `--verbose`, `-v` | Show detailed output including per-filter drop counts |
| `--no-color` | Disable colored output (also respects `NO_COLOR` env var) |
| `--config` | Config file path (default: `~/.unlog/config.toml`) |

### Exit codes

| Code | Meaning |
|------|---------|
| `0` | The report was written |
| `1` | An error (bad flag, missing or unreadable input, unwritable `--output`); no report |
| `130` | Interrupted; no report |

## Configuration

Settings are resolved in priority order: **CLI flags > environment variables > config file > defaults**.

### Config file

Create `~/.unlog/config.toml`:

```toml
level = "warn"
format = "text"
since = ""             # start time filter (ISO 8601 or relative, e.g. "2h")
until = ""             # end time filter
tz = ""                # zone of timestamps that carry none (default: UTC)
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
| `UNLOG_NOISE_FILE` | Custom noise patterns path |
| `UNLOG_VERBOSE` | Verbose output (`true`/`false`) |
| `UNLOG_SINCE` | Start time filter |
| `UNLOG_UNTIL` | End time filter |
| `UNLOG_TZ` | Zone of timestamps that carry none |
| `UNLOG_NO_COLOR` | Disable color (`true`/`false`) |
| `NO_COLOR` | Disable color (any value, per [no-color.org](https://no-color.org)) |

The settings of the LLM stage removed in v0.4.0 (`ai_provider`, `model`, `system_prompt` and `ai_timeout` in the config file, and `UNLOG_AI_PROVIDER`, `UNLOG_MODEL`, `UNLOG_SYSTEM_PROMPT` and `UNLOG_AI_TIMEOUT`) have no effect; when one is set to a value, unlog says so on stderr.

## Supported log formats

unlog auto-detects the format of each input file by sampling lines and using majority vote. No configuration needed.

| Format | Example |
|--------|---------|
| **JSON** | `{"level":"error","msg":"timeout","ts":"2024-01-15T10:00:00Z"}` |
| **logfmt** | `ts=2024-01-15T10:00:00Z level=error msg="timeout"` |
| **CSV** | `2024-01-15T10:00:00Z,ERROR,timeout,api` (with header row) |
| **Syslog RFC 3164** | `<131>Jan 15 10:00:00 app-1 myapp[1234]: ERROR timeout` |
| **Syslog files** | `Jan 15 10:00:00 app-1 myapp[1234]: ERROR timeout` or `2024-01-15T10:00:00.123456+00:00 app-1 myapp[1234]: ...` (what rsyslog writes to `/var/log`) |
| **Syslog RFC 5424** | `<165>1 2024-01-15T10:00:00Z host app 1234 - - timeout` |
| **Apache CLF** | `10.0.0.1 - - [15/Jan/2024:10:00:00 -0700] "GET /api HTTP/1.1" 500 789` |
| **Docker JSON** | `{"log":"timeout\n","stream":"stderr","time":"2024-01-15T..."}` |
| **Docker Compose** | `api-1  \| 2024-01-15 10:00:00 ERROR timeout` |
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

unlog uses a 5-stage streaming pipeline connected by buffered Go channels:

![Pipeline](docs/pipeline.png)

| Stage | Package | Description |
|-------|---------|-------------|
| 1. Ingest | `ingest/` | Read files, detect format, parse into structured entries |
| 2. Filter | `filter/` | Level filter, time window, noise removal, one record per error, burst detection |
| 3. Enrich | `enrich/` | Error chain detection, deployment events, cross-source correlation, field extraction |
| 4. Compact | `compact/` | Priority scoring, token-budgeted compaction |
| 5. Render | `internal/render/` | Terminal, JSON, and Markdown output |

No stage makes a network call. Stages 1-4 are public packages importable as a Go library.

## Custom noise patterns

Create a text file with one pattern per line (case-insensitive substring match). Lines starting with `#` are comments. Patterns apply only to entries below ERROR: an error or fatal entry is always kept, even when its text matches.

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
  "analysis_duration_ms": 1234,
  "stats": {
    "ingested": 50000,
    "dropped": 49958,
    "survived": 42,
    "unique_signatures": 40,
    "time_window_start": "2024-01-15T10:00:00Z",
    "time_window_end": "2024-01-15T10:05:00Z",
    "auto_detected_window": false,
    "source_breakdown": { "api.log": 3000, "db.log": 12 },
    "file_count": 0,
    "bytes_processed": 0,
    "duration_ms": 310
  },
  "compacted_summary": "## Incident Overview\n..."
}
```

Every key is always present. `analysis_duration_ms` is how long the whole run took. `survived` counts the distinct errors (one per source and service) that reach the summary, and `dropped` includes the merged repeats, so `ingested = dropped + survived`. `source_breakdown` counts ERROR and FATAL occurrences per source. `auto_detected_window` is always `false` (unlog never narrows the time range on its own; use `--since`/`--until`). The `--output` path is checked before any log is read. The AI keys `model_used`, `ai_duration_ms`, `analysis` and `analysis_error` were removed in v0.4.0, along with the LLM stage that filled them.

## Development

Needs [just](https://just.systems) (bare `just` lists every recipe),
optionally [golangci-lint](https://golangci-lint.run), and
[jq](https://jqlang.org) for `just plugin-version`.

```bash
just build           # bin/unlog
just install         # go install . into $GOBIN
just test            # all tests, with the race detector
just lint            # golangci-lint
just golden          # rewrite testdata/golden/ after an intended output change
just smoke           # build, then run against testdata/incidents/
just bench           # all benchmarks
just bench-compare   # benchmarks x6 into bench-new.txt, for benchstat
just bigdata         # 100MB synthetic logs in testdata/big/
just plugin-version 0.4.0  # set the plugin's version and marketplace pin
just clean           # remove bin/ and generated test data
```

To run one package or test: `go test ./ingest/ -run TestIngester`.

### Releasing

A release is a pushed `vX.Y.Z` tag; `.github/workflows/release.yml` builds and publishes it with GoReleaser.

1. Run `just plugin-version X.Y.Z` and commit. The marketplace entry on `main` now points at the new tag, so the plugin can't be installed until the tag is pushed: tag right after.
2. Write the release notes to a file whose first line is the title (`unlog vX.Y.Z`), then a blank line, then the notes.
3. Run `git tag -a --cleanup=whitespace vX.Y.Z -F notes.md`, then push the commit and the tag. Without `--cleanup=whitespace`, git drops every line that starts with `#`, Markdown headings included.

The workflow fails before publishing when the tag, the plugin's version and the marketplace pin disagree (`scripts/check-plugin-version`), or when the tag isn't annotated or its message has no notes (`scripts/release-notes`). It publishes the tag's message, minus its first line, as the release notes.

### Generating test data

Use `testdata/big/generate.go` to create synthetic log files for benchmarking and manual testing. See its [README](testdata/big/README.md) for usage, flags, and examples.

## License

MIT
