.PHONY: bench bench-record bench-save bench-compare test build run prep install-tools

install-tools:
	go install golang.org/x/perf/cmd/benchstat@latest

bench:
	go test -bench . -benchmem ./benchmarks/...

bench-record:
	@echo "Running benchmarks and saving to bench-latest.txt..."
	go test -bench . -benchmem -count=5 ./benchmarks/... > bench-latest.txt
	@echo "Saved to bench-latest.txt"

bench-save:
	@if [ -f bench-latest.txt ]; then \
		cp bench-latest.txt bench-prev.txt; \
		echo "Backed up bench-latest.txt to bench-prev.txt"; \
	else \
		echo "No bench-latest.txt found. Run 'make bench-record' first."; \
	fi

bench-compare:
	@if [ ! -f bench-prev.txt ]; then \
		echo "No bench-prev.txt found. Run 'make bench-save' then 'make bench-record' to create comparison points."; \
	else \
		benchstat bench-prev.txt bench-latest.txt; \
	fi

test:
	go test ./...

build:
	go build -o search bin/main.go

run:
	go run main.go

prep: test bench build
