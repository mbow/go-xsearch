# TODO

## Benchmark follow-up

Date: 2026-04-17

Reference:
`docs/benchmarks/bench-latest.txt`

Command:
`GOWORK=off go test -bench . -benchmem -count=5 ./benchmarks/...`

Investigate these benchmark regressions before updating the stored benchmark baseline:

- `BenchmarkEngine_Search_CachedPrefix`: `33.68 ns/op` vs `28.35 ns/op` (`+18.80%`)
- `BenchmarkComponent_EngineShortQuery`: `34.06 ns/op` vs `28.50 ns/op` (`+19.51%`)
- `BenchmarkParallel_EngineSearch`: `16.22 us/op` vs `14.99 us/op` (`+8.23%`)
- `BenchmarkParallel_HTTPSearch`: `1.265 us/op` vs `1.213 us/op` (`+4.29%`)

Most other benchmarks were flat or improved in the same run. After the follow-up work, rerun `benchstat docs/benchmarks/bench-latest.txt <new-run>` and decide whether to refresh the checked-in benchmark snapshot.
