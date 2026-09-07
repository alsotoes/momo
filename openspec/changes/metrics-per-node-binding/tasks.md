# Tasks: metrics-per-node-binding — per-node bind for `/metrics` (#941)

## 1. Config surface (`src/common`)
- [x] `struct.go`: add `Daemon.MetricsBindHost string`, `Daemon.MetricsBindPort int`;
      add `ConfigurationMetrics.PrometheusBindHost string`
- [x] `config.go`: parse+validate `[daemon.N] metrics_host`/`metrics_port`
      (port 1..65535 else EINVAL); parse `[metrics] prometheus_bind_host`

## 2. Exporter (`src/server`)
- [x] `metrics_exporter.go`: `StartMetricsServer(ctx, host string, port int, collector)`;
      bind `net.JoinHostPort(host, strconv.Itoa(port))`; empty host → `:port`;
      keep `port<=0` disabled
- [x] `server.go`: resolve per-node (override → global default → bind); call new signature

## 3. Tests
- [x] `config_test.go`: overrides + fallback + invalid-port rejection (MPC-T1)
- [x] `metrics_exporter_test.go`: bind host:port + /metrics + /health (MPC-T2);
      same-host distinct ports (MPC-T3); collision no-panic (MPC-T4)
- [x] goleak + `-race` (MPC-T5)

## 4. Docs (Rule 27)
- [x] `docs/CONFIGURATION.md`: `[metrics] prometheus_bind_host`,
      `[daemon.N] metrics_host`/`metrics_port`
- [x] `conf/momo.conf` example
- [x] `docs/ARCHITECTURE.md` metrics section note

## 5. Validation
- [x] `go fmt`, `go vet`, `go build`, `go test` (common + server)
- [x] `go work sync` + vendor parity
- [x] CI: `make test` green
