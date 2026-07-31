## 1. Configuration
- [ ] 1.1 Add `ClientSideReplicationModes []int` field to `ConfigurationGlobal` in `src/common/struct.go`
- [ ] 1.2 Parse `client_side_replication_modes` in `config.go` with zero-alloc CSV parser (same pattern as `replication_order`)
- [ ] 1.3 Default to `[3]` if config key is absent
- [ ] 1.4 Add `client_side_replication_modes=3` to `conf/momo.conf`

## 2. External Client Detection
- [ ] 2.1 In `s3_communicator.go` `HandshakeServer`, if `X-Momo-Requested-Mode` is absent, set `timestamp = DummyEpoch`
- [ ] 2.2 Add a boolean field to `S3Communicator` to track "external client" status

## 3. Per-Transaction Mode Downgrade
- [ ] 3.1 In `server.go`, after mode selection, if connection is external and mode is in `ClientSideReplicationModes`, walk `ReplicationOrder` forward to find next mode NOT in that list
- [ ] 3.2 Use the downgraded mode for this transaction only — do not mutate global polymorphic state
- [ ] 3.3 Log the downgrade for audit traceability

## 4. Verification
- [ ] 4.1 Unit test: external client detection (no `X-Momo-Requested-Mode` → `DummyEpoch`)
- [ ] 4.2 Unit test: mode downgrade skips client-side modes correctly
- [ ] 4.3 Unit test: momo CLI client unaffected (DummyEpoch → no downgrade)
- [ ] 4.4 Unit test: default `client_side_replication_modes` when config absent
- [ ] 4.5 E2E test: aws-cli PUT to server in primary-splay mode → file replicated via splay
- [ ] 4.6 E2E test: momo CLI PUT to same server → primary-splay used as normal
