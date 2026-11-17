# Tasks

- [x] 1.1 `[storage] multipart_upload_ttl` (duration, default 24h, 0 disables)
- [x] 1.2 Reject negative and unparseable values at load
- [x] 1.3 Reap expired sessions and their spill directories
- [x] 1.4 Sweep orphaned spill directories (marker-proven, no symlinks, skipping live sessions)
- [x] 1.9 Parts live under `<data>/multipart-spill` (private, random names); copy when the spill is on another filesystem
- [x] 1.5 Start at boot and every 10 minutes; context-cancelled, panic-guarded
- [x] 1.6 Wire through `startBackgroundMaintenance` (one call in the daemon)
- [x] 1.7 Tests: expiry, fresh session, disabled, orphan old/recent, live protection,
      loop start/stop, TTL parser, config validation
- [x] 1.8 Docs: CONFIGURATION, `conf/momo.conf`, ARCHITECTURE, TESTING
