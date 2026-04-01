---
description: Run the benchmark suite and report performance changes
---

Follow these steps exactly when the user asks you to "run the benchmarks and give me the change". 

This workflow uses `benchstat` to compare benchmark results between different commits. Each benchmark file includes a metadata header with the commit SHA, branch, and timestamp.

1. **Prerequisites Checklist**
Ensure `benchstat` is installed:
`make install-tools`

2. **Establish the Comparison State**
Evaluate the user's intent:
- **Scenario A:** Save the current state as the comparison baseline (before making changes):
  ```bash
  make bench-record
  make bench-save
  ```
  This creates `bench-prev-<SHA>.txt` and symlinks `bench-prev.txt` to it.

- **Scenario B (Most Common):** Code changes have been made. Record new benchmarks and compare:
  ```bash
  make bench-record
  make bench-compare
  ```
  The compare target will **refuse** to run if `bench-prev` and `bench-latest` are from the same commit. If this happens, it means no code changes were made — there's nothing to compare.

- **Scenario C:** Compare against the original pristine project baseline (historical progress):
  ```bash
  make bench-record
  make bench-compare-baseline
  ```

3. **Compare and Output the Differences**
`make bench-compare` prints the commit SHAs being compared (e.g., `Comparing abc1234 -> def5678`) followed by the benchstat output.

4. **Summarize for the User**
Read the output and present a clean summary:
- **Performance Regressions:** Highlight any benchmarks that got slower, with the component name and percentage.
- **Performance Improvements:** Highlight components that got faster.
- **Unchanged:** Note paths that are stable (marked `~` by benchstat).
- Use benchstat's p-values: only report changes where p < 0.05 as statistically significant.

5. **Important: Same-commit guard**
If `make bench-compare` errors with "same commit", it means `bench-prev.txt` was recorded from the same code as `bench-latest.txt`. This is a meaningless comparison. Tell the user they need to either:
- Make code changes first, then `make bench-record && make bench-compare`
- Or save a baseline from a different commit: checkout the old code, `make bench-record && make bench-save`, then checkout back and `make bench-record && make bench-compare`
