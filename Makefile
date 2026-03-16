.PHONY: build test smoke lint bench bench-compare clean install bigdata

build:
	go build -o bin/unlog .

test:
	go test ./... -race -count=1

lint:
	golangci-lint run

smoke: build
	./bin/unlog testdata/incidents/db_connection/
	./bin/unlog --format json testdata/incidents/deploy_failure/
	./bin/unlog stats testdata/incidents/db_connection/
	./bin/unlog formats testdata/formats/

bigdata:
	@mkdir -p testdata/big
	go run testdata/big/generate.go --size 100MB --format json --error-rate 0.05 --output testdata/big/test.log
	go run testdata/big/generate.go --size 100MB --format json --error-rate 0.05 --output testdata/big/test.tar.gz

bench:
	go test ./... -bench=. -benchmem -run=^$$

bench-compare:
	go test ./... -bench=. -benchmem -run=^$$ -count=6 | tee bench-new.txt
	@echo "Run 'benchstat bench-old.txt bench-new.txt' to compare"

clean:
	rm -rf bin/ testdata/large/
	rm -f testdata/big/*.log

install:
	go install .
