# Show all recipes
default:
    @just --list

# Build the CLI to bin/unlog
build:
    go build -o bin/unlog .

# Install the CLI into $GOBIN (or $GOPATH/bin)
install:
    go install .

# Run all tests with the race detector
test:
    go test ./... -race -count=1

# Rewrite testdata/golden/ from the current output; review the diff after
golden:
    go test -count=1 -run TestE2EGoldenSummaries . -update

# Run golangci-lint
lint:
    golangci-lint run

# Build, then run the CLI against the bundled incident fixtures
smoke: build
    ./bin/unlog testdata/incidents/db_connection/
    ./bin/unlog --format json testdata/incidents/deploy_failure/

# Run all benchmarks once
bench:
    go test ./... -bench=. -benchmem -run='^$'

# Run all benchmarks 6 times into bench-new.txt, for benchstat
bench-compare:
    go test ./... -bench=. -benchmem -run='^$' -count=6 | tee bench-new.txt
    @echo "Run 'benchstat bench-old.txt bench-new.txt' to compare"

# Generate 100MB of synthetic JSON logs in testdata/big/, plain and as tar.gz
bigdata:
    mkdir -p testdata/big
    go run testdata/big/generate.go --size 100MB --format json --error-rate 0.05 --output testdata/big/test.log
    go run testdata/big/generate.go --size 100MB --format json --error-rate 0.05 --output testdata/big/test.tar.gz

# Remove build output and generated test data
clean:
    rm -rf bin/
    rm -f testdata/big/*.log testdata/big/*.gz testdata/big/*.tgz
