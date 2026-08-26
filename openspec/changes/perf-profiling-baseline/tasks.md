# Tasks: perf-profiling-baseline — benchmark segments + file-based profiling (#948)

## 1. Benchmark segments (Rule 27 / Rule 73 enhancement)
- [x] `src/common/hash_bench_test.go`: BenchmarkHashBytes + BenchmarkHashFile,
      3 sizes each, SetBytes (PB-T1)
- [x] `src/storage/bench_test.go`: BenchmarkLocalWrite, BenchmarkReadVerify,
      BenchmarkS3PutSpool, 3 sizes each, SetBytes (PB-T2)
- [ ] Verify benchmarks compile + run, benchstat-compatible
      (`go test -run '^$' -bench ... -benchmem`) (PB-T3)

## 2. Steering rule (`openspec/config.yaml`)
- [ ] Append **Rule 75 (No Networked pprof)**: profiling via Go built-in
      file-based `-cpuprofile/-memprofile/-blockprofile` on the test binary; no
      `net/http/pprof` on the unauthenticated data-path listener (RCE-class
      surface); any future admin endpoint isolated to loopback/Unix socket +
      explicit enable + TLS when off-loopback. Extends Rule 74 (PB-T4)

## 3. OpenSpec set (Rule 11 / Rule 73)
- [x] Author `openspec/changes/perf-profiling-baseline/{proposal,spec,tasks}`
      linked to issue #948 (PB-T5)

## 4. Validation
- [ ] Profile capture proven with no listener:
      `go test -run '^$' -bench Hash -cpuprofile /tmp/cpu.prof -memprofile /tmp/mem.prof`
      → valid pprof, `go tool pprof /tmp/cpu.prof` works (PB-T6)
- [ ] `go vet` + `go test -race -cover` green for the touched packages (PB-T7)
- [ ] `git diff master...HEAD --name-only` = bench files + OpenSpec set +
      config.yaml only (PB-T8)
- [ ] Initial baseline numbers posted to issue #948 for Win1/2/3 (PB-T9)
