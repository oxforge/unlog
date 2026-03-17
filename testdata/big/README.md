# Big Data Test Generator

Generate large log files for manual performance and correctness testing of `unlog`.

## Usage

```bash
# Generate 100MB JSON log file (default)
make bigdata

# Generate custom size/format
go run testdata/big/generate.go --size 1GB --format json --error-rate 0.1 --output testdata/big/large.log

# Generate logfmt format
go run testdata/big/generate.go --size 500MB --format logfmt --output testdata/big/logfmt.log

# Generate CSV format (includes header row)
go run testdata/big/generate.go --size 500MB --format csv --output testdata/big/csv.log

# Generate to stdout and pipe to unlog
go run testdata/big/generate.go --size 50MB --format json | ./bin/unlog analyze -
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--size` | `100MB` | Target file size (B, KB, MB, GB) |
| `--format` | `json` | Log format: `json`, `logfmt`, `syslog`, `clf`, `csv`, `generic` |
| `--error-rate` | `0.05` | Fraction of ERROR/FATAL entries (0.0-1.0) |
| `--output` | stdout | Output file path |

## Testing with unlog

```bash
# Build unlog first
make build

# Basic analysis
./bin/unlog analyze testdata/big/test.log

# With verbose stats
./bin/unlog analyze testdata/big/test.log --verbose

# Error-only filtering
./bin/unlog analyze testdata/big/test.log --level error

# Time performance
time ./bin/unlog analyze testdata/big/test.log --level error > /dev/null
```

## Generated Data Characteristics

- Timestamps progress forward with small random gaps (10-100ms)
- 6 service names for cross-source correlation testing
- Middle 20% of file contains an "incident spike" with higher error rate
- Chain error sequences (DB exhaustion, OOM cascade, deployment failure) injected during spikes
- Error entries include HTTP status codes and trace IDs for field extraction testing

## Notes

- Generated `.log` files are gitignored
- For reproducible benchmarks, save the generated file and reuse it
