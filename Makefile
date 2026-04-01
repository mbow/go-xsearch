.PHONY: bench bench-record bench-save bench-compare bench-compare-baseline test build run prep install-tools

BENCH_DIR     = docs/benchmarks
BENCH_LATEST  = $(BENCH_DIR)/bench-latest.txt
BENCH_COUNT   = 5

install-tools:
	go install golang.org/x/perf/cmd/benchstat@latest

bench:
	go test -bench . -benchmem ./benchmarks/...

# Record benchmarks with commit metadata header.
bench-record:
	@echo "Running benchmarks (count=$(BENCH_COUNT))..."
	@SHA=$$(git rev-parse --short HEAD) && \
	BRANCH=$$(git branch --show-current) && \
	DIRTY=$$(git diff --quiet && echo "" || echo " (dirty)") && \
	{ \
		echo "# commit: $$SHA  branch: $$BRANCH$$DIRTY  date: $$(date -Iseconds)"; \
		echo ""; \
		go test -bench . -benchmem -count=$(BENCH_COUNT) ./benchmarks/...; \
	} > $(BENCH_LATEST)
	@echo "Saved to $(BENCH_LATEST)"

# Save current latest as the comparison baseline (named with commit SHA).
bench-save:
	@if [ ! -f $(BENCH_LATEST) ]; then \
		echo "No $(BENCH_LATEST) found. Run 'make bench-record' first."; \
		exit 1; \
	fi
	@SHA=$$(head -1 $(BENCH_LATEST) | grep -oP 'commit: \K[a-f0-9]+') && \
	if [ -z "$$SHA" ]; then \
		echo "ERROR: $(BENCH_LATEST) has no commit header. Re-run 'make bench-record'."; \
		exit 1; \
	fi && \
	DEST="$(BENCH_DIR)/bench-prev-$$SHA.txt" && \
	cp $(BENCH_LATEST) "$$DEST" && \
	ln -sf "bench-prev-$$SHA.txt" $(BENCH_DIR)/bench-prev.txt && \
	echo "Saved as $$DEST (symlinked from bench-prev.txt)"

# Compare latest against prev, refusing to compare identical commits.
bench-compare:
	@if [ ! -L $(BENCH_DIR)/bench-prev.txt ] && [ ! -f $(BENCH_DIR)/bench-prev.txt ]; then \
		echo "No bench-prev.txt found. Run 'make bench-save' then 'make bench-record'."; \
		exit 1; \
	fi
	@if [ ! -f $(BENCH_LATEST) ]; then \
		echo "No $(BENCH_LATEST) found. Run 'make bench-record' first."; \
		exit 1; \
	fi
	@PREV_SHA=$$(head -1 $(BENCH_DIR)/bench-prev.txt | grep -oP 'commit: \K[a-f0-9]+') && \
	LATEST_SHA=$$(head -1 $(BENCH_LATEST) | grep -oP 'commit: \K[a-f0-9]+') && \
	if [ "$$PREV_SHA" = "$$LATEST_SHA" ]; then \
		echo "ERROR: bench-prev ($$PREV_SHA) and bench-latest ($$LATEST_SHA) are from the same commit."; \
		echo "Nothing to compare — make changes first, then 'make bench-record'."; \
		exit 1; \
	fi && \
	echo "Comparing $$PREV_SHA -> $$LATEST_SHA" && \
	benchstat $(BENCH_DIR)/bench-prev.txt $(BENCH_LATEST)

# Compare latest against the original pristine baseline (historical).
bench-compare-baseline:
	@if [ ! -f $(BENCH_LATEST) ]; then \
		echo "No $(BENCH_LATEST) found. Run 'make bench-record' first."; \
		exit 1; \
	fi
	benchstat $(BENCH_DIR)/baseline.txt $(BENCH_LATEST)

test:
	go test ./...

build:
	go build -o search bin/main.go

run:
	go run main.go

prep: test bench build
