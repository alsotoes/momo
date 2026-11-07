# Tasks: R11 gossip load signal

- [x] Message type `MsgLoad` and fixed 20-byte `PeerLoad` payload with encode/decode
- [x] Decoder rejects short, oversized, and out-of-range reports
- [x] `Peer.SetLoad` / `Peer.Load` with an atomic pointer (no lock on the read path)
- [x] `Gossiper.SetLoadProvider`, publication on the heartbeat fanout, `Gossiper.PeerLoad`
- [x] `MsgLoad` handled in the RPC dispatch with panic recovery
- [x] Daemon sampler: disk utilisation from storage, in-flight from metrics (queue reserved)
- [x] Unit tests: payload round trip, malformed input, publication with/without provider, storage, unknown peer, sampler fallbacks
- [x] Documentation: architecture section and engineering-journal post
- [ ] Redirect contract and admission rule (tracked in #1261)
