---
description: Run the benchmark suite and report performance changes
---

Follow these steps exactly when the user asks you to "run the benchmarks and give me the change". 

This workflow uses `benchstat` to compare the baseline benchmark times against the latest code execution times.

1. **Prerequisites Checklist**
Ensure the benchmarking tool `benchstat` is installed on the machine:
// turbo
`make install-tools`

2. **Establish the Comparison State**
Evaluate the user's intent:
- **Scenario A:** If the user is currently looking at the `main` branch or a clean state and wants to save *this* as the local baseline to compare *later*, you must record the baseline and save it:
  ```bash
  make bench-record
  make bench-save
  ```
- **Scenario B:** (Most Common) If the user has *already* made code changes and wants to see how they affect performance against the saved local baseline (`docs/benchmarks/bench-prev.txt`), you just need to run the new benchmarks:
// turbo
`make bench-record`
- **Scenario C:** If the user wants to compare strictly against the original pristine project baseline, run the benchmarks and then compare against `baseline.txt`:
```bash
make bench-record
make bench-compare-baseline
```

3. **Compare and Output the Differences**
Run the comparison between the saved baseline and the latest run:
// turbo
`make bench-compare` (or `make bench-compare-baseline` if Scenario C)

4. **Summarize for the User**
Read the system output from the `make bench-compare` execution and present a clean, organized summary to the user. Describe all results clearly:
- **Performance Regressions:** Highlight any benchmarks that got slower, specifying the exact component (e.g., ShortQuery cache, HTTP server).
- **Performance Improvements:** Highlight components that got faster.
- Use `benchstat` statistical significance values (e.g. +/- percentages) and explain what they mean in plain English.
