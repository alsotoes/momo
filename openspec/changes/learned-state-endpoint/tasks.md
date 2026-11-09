# Tasks — Learned State in /health/detailed

## Implementation
- [ ] `src/metrics/adaptive.go`: expose `AdaptivePersistence() (store string, ok bool)`
- [ ] `src/server/learned_state.go`: pure `buildLearnedState(cfg, serverId)` returning
      controller / replication_mode / adaptive blocks
- [ ] `src/server/metrics_exporter.go`: `LearnedState` provider field, `SetLearnedState`,
      include the block in `detailedHealthHandler` (omit when unset)
- [ ] `src/server/server.go`: install the provider where the collector is created

## Tests
- [ ] Provider-nil omission (backward compatibility)
- [ ] Election on/off and holder/non-holder
- [ ] Adaptive on/off with learned values, persistence flag
- [ ] Mode/order/index from configuration
- [ ] No absolute paths in any rendered value

## Documentation
- [ ] `docs/ARCHITECTURE.md`: "State, Persistence, and What Survives a Restart"
- [ ] `docs/GUIDES/CONFIGURATION.md`: cross-link
- [ ] ADR via `make adr-sync` + `.no-blog.md` justification (Rule 76)
