# Tasks: s3-listxml-appendformat — eliminate heap allocations in ListObjectsV2 XML (#900)

## 1. Serialization (`src/transport/s3_communicator.go`)
- [ ] In `FormatListObjectsV2XML`: declare `var timeBuf [32]byte`; in the
      `emitContents` closure replace `buf.WriteString(formatLastModified(file.ModTime))`
      with `buf.Write(t.AppendFormat(timeBuf[:0], "2006-01-02T15:04:05.000Z"))`
      where `t := time.Unix(0, file.ModTime).UTC()` (LXF-T1)

## 2. Benchmark (`src/transport/s3_xml_bench_test.go`)
- [ ] `BenchmarkFormatListObjectsV2XML_AppendFormat`: `ReportAllocs`, 1000
      synthetic `common.FileMetadata`, call `FormatListObjectsV2XML` (LXF-T2)
- [ ] `BenchmarkFormatListObjectsV2XML_OldFormat`: reference `time.Format` path
      for allocation comparison (LXF-T3)

## 3. Docs (Rule 27 / Rule 73)
- [x] Author `openspec/changes/s3-listxml-appendformat/{proposal,spec,tasks}` linked
      to issue #900
- [ ] `docs/PERFORMANCE.md` benchmarks table updated by pre-commit hook (Rule 61)

## 4. Validation
- [x] `go vet`, `go build` in `src/transport`
- [x] `go test -race -run 'ListObjectsV2'` in `src/transport`
- [x] `go test -race` full `src/transport` (230 passed)
- [ ] CI green including `benchstat` and `review`
