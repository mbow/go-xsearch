.PHONY: bench bench-record bench-save bench-compare test build run prep install-tools

install-tools:
	go install golang.org/x/perf/cmd/benchstat@latest

bench:
	go test -bench . -benchmem ./benchmarks/...

bench-record:
	@echo "Running benchmarks and saving to docs/benchmarks/bench-latest.txt..."
	go test -bench . -benchmem -count=5 ./benchmarks/... > docs/benchmarks/bench-latest.txt
	@echo "Saved to docs/benchmarks/bench-latest.txt"

bench-save:
	@if [ -f docs/benchmarks/bench-latest.txt ]; then \
		cp docs/benchmarks/bench-latest.txt docs/benchmarks/bench-prev.txt; \
		echo "Backed up docs/benchmarks/bench-latest.txt to docs/benchmarks/bench-prev.txt"; \
	else \
		echo "No docs/benchmarks/bench-latest.txt found. Run 'make bench-record' first."; \
	fi

bench-compare:
	@if [ ! -f docs/benchmarks/bench-prev.txt ]; then \
		echo "No docs/benchmarks/bench-prev.txt found. Run 'make bench-save' then 'make bench-record' to create comparison points."; \
	else \
		benchstat docs/benchmarks/bench-prev.txt docs/benchmarks/bench-latest.txt; \
	fi

bench-compare-baseline:
	@if [ ! -f docs/benchmarks/bench-latest.txt ]; then \
		echo "No docs/benchmarks/bench-latest.txt found. Run 'make bench-record' first."; \
	else \
		benchstat docs/benchmarks/baseline.txt docs/benchmarks/bench-latest.txt; \
	fi

test:
	go test ./...

build:
	go build -o search bin/main.go

run:
	go run main.go

prep: test bench build
