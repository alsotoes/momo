# Tasks: metrics-per-node-binding — per-node bind for `/metrics` (#941)

## 1. Config surface (`src/common`)
- [ ] `struct.go`: add `Daemon.MetricsBindHost string`, `Daemon.MetricsBindPort int`;
      add `ConfigurationMetrics.PrometheusBindHost string`
- [ ] `config.go`: parse+validate `[daemon.N] metrics_host`/`metrics_port`
      (port 1..65535 else EINVAL); parse `[metrics] prometheus_bind_host`

## 2. Exporter (`src/server`)
- [ ] `metrics_exporter.go`: `StartMetricsServer(ctx, host string, port int, collector)`;
      bind `net.JoinHostPort(host, strconv.Itoa(port))`; empty host → `:port`;
      keep `port<=0` disabled
- [ ] `server.go`: resolve per-node (override → global default → bind); call new signature

## 3. Tests
- [ ] `config_test.go`: overrides + fallback + invalid-port rejection (MPC-T1)
- [ ] `metrics_exporter_test.go`: bind host:port + /metrics + /health (MPC-T2);
      same-host distinct ports (MPC-T3); collision no-panic (MPC-T4)
- [ ] goleak + `-race` (MPC-T5)

## 4. Docs (Rule 27)
- [ ] `docs/CONFIGURATION.md`: `[metrics] prometheus_bind_host`,
      `[daemon.N] metrics_host`/`metrics_port`
- [ ] `conf/momo.conf` example
- [ ] `docs/ARCHITECTURE.md` metrics section note

## 5. Validation
- [ ] `go fmt`, `go vet`, `go build`, `go test` (common + server)
- [ ] `go work sync` + vendor parity
- [ ] CI: `make test` green
