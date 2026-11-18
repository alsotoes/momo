# Multipart Upload Footprint Metrics

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1369

## Why

Since #1358 multipart parts live on disk under `<data>/multipart-spill`, bounded
by `multipart_upload_ttl` since #1361. Neither the number of open uploads nor the
disk they hold was visible: "is this node's data dir growing from uploads or from
objects?" could only be answered by reading AUDIT lines, and a node filling its
disk with abandoned uploads looked exactly like normal object growth.

## What Changes

- Two scrape-time gauges derived from the session map (no directory walk per
  scrape):

  | Metric | Meaning |
  |---|---|
  | `momo_multipart_uploads_active` | open multipart uploads on this node |
  | `momo_multipart_spill_bytes` | bytes held by their spooled parts |

- Both families are emitted even at zero, so a dashboard distinguishes "none" from
  "not reported".
- Grafana panel "Multipart Upload Footprint" (open uploads + spill MiB).

## Impact

- Affected specs: `multipart-metrics`
- Affected code: `src/transport/s3_multipart_reaper.go` (`MultipartStats`),
  `src/server/metrics_exporter.go`
- Observability only: no behaviour change, no config key.
