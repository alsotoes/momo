# Tasks

- [x] 1.1 Manifest written atomically into the spill dir (0600, temp+rename)
- [x] 1.2 Written at create, and after every accepted part
- [x] 1.3 `RecoverMultipartSessions` rebuilds the map with stable ids
- [x] 1.4 Validate: unparsable/incomplete manifest, missing or truncated part
- [x] 1.5 Expired sessions are not recovered; a live session wins a duplicate id
- [x] 1.6 Recovery runs at startup, before the first reaper sweep
- [x] 1.7 Tests: restart round-trip, expiry, missing part, malformed, atomic write,
      no spill root, end-to-end complete after recovery
- [x] 1.8 Live daemon: kill during an upload, restart, complete, bytes match
- [x] 1.9 Docs: ARCHITECTURE, TESTING, COMPATIBILITY note
