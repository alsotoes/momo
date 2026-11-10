package p2p

import (
	"log"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

// Hinted handoff (R6 v2, #1142). When a metadata replica is unreachable during
// a quorum write or delete, the coordinator stores the record locally and
// replays it when SWIM reports the peer alive again. Without this, a replica
// that was down at write time only converges on the next read (read repair) or
// the next write of the same name — and a delete tombstone could be lost for
// longer than an operator expects.
const (
	// MetadataHintTTL bounds how long a hint is retained. Beyond this the peer
	// has been unreachable long enough that the record must be re-resolved from
	// a live replica (read repair) rather than replayed blindly.
	MetadataHintTTL = time.Hour
	// maxMetadataHintsPerPeer caps the bucket for one peer. Older hints are
	// pruned first; reaching this bound is audited because it means the peer has
	// been down for a long time.
	maxMetadataHintsPerPeer = 4096

	metadataHintKindReplicate byte = 0
	metadataHintKindDelete    byte = 1
)

// storeMetadataHint records one pending record for a peer. Failures are audited
// but never fail the caller: the write quorum already succeeded, so a hint-store
// failure only weakens convergence, not durability of the acknowledged write.
func (m *MetadataRPCProvider) storeMetadataHint(peer int32, name string, kind byte, args []byte) {
	if m.store == nil || len(args) == 0 {
		return
	}
	now := time.Now()
	if _, err := m.store.PruneMetadataHints(now.Add(-MetadataHintTTL)); err != nil {
		log.Printf("AUDIT: metadata hint prune failed: %v (errno=%d)", err, syscall.EIO)
	}
	existing, err := m.store.ListMetadataHints(peer)
	if err != nil {
		log.Printf("AUDIT: metadata hint list failed for peer %d: %v (errno=%d)", peer, err, syscall.EIO)
	} else if len(existing) >= maxMetadataHintsPerPeer {
		log.Printf("AUDIT: metadata hint cap reached for peer %d (%d hints): dropping stale hints", peer, len(existing))
		dropOldestHints(m, peer, existing, len(existing)-maxMetadataHintsPerPeer+1)
	}
	payload := make([]byte, 1+len(args))
	payload[0] = kind
	copy(payload[1:], args)
	if err := m.store.PutMetadataHint(peer, name, payload, now); err != nil {
		log.Printf("AUDIT: metadata hint store failed for peer %d name %q: %v (errno=%d)", peer, name, err, syscall.EIO)
	}
}

// dropOldestHints removes n oldest hints for a peer to keep the bucket bounded.
func dropOldestHints(m *MetadataRPCProvider, peer int32, hints []storage.MetadataHint, n int) {
	if n <= 0 {
		return
	}
	if n > len(hints) {
		n = len(hints)
	}
	for _, h := range hints[:n] {
		if err := m.store.DeleteMetadataHint(peer, h.Name); err != nil {
			log.Printf("AUDIT: metadata hint drop failed for peer %d name %q: %v (errno=%d)", peer, h.Name, err, syscall.EIO)
		}
	}
}

// ReplayAllHints replays pending hints for every peer the transport currently
// considers ALIVE. A hint is normally replayed on the ALIVE transition, but that
// event can be missed (a restart, or a hook installed after the peer returned),
// which would strand the hint until the peer flapped again. The sweep is bounded
// by the hint TTL and per-peer cap.
func (m *MetadataRPCProvider) ReplayAllHints() (peers int) {
	if m.store == nil || m.transport == nil {
		return 0
	}
	defer common.RecoverClose("MetadataRPC.ReplayAllHints", nil) //nolint:staticcheck // no resource to close; log-only guard
	pm := m.transport.Peers()
	if pm == nil {
		return 0
	}
	alive := make(map[int32]bool)
	for _, p := range pm.Alive() {
		alive[p.ID] = true
	}
	pending, err := m.store.ListMetadataHintPeers()
	if err != nil {
		log.Printf("AUDIT: metadata hint peer list failed: %v (errno=%d)", err, syscall.EIO)
		return 0
	}
	for _, peer := range pending {
		if !alive[peer] {
			continue
		}
		m.ReplayMetadataHints(peer)
		peers++
	}
	return peers
}

// ReplayMetadataHints sends every hint owed to a peer that just came back, and
// deletes each one only after the peer acknowledges. Idempotent: a replayed
// record carrying an older vector clock is ignored by the replica.
func (m *MetadataRPCProvider) ReplayMetadataHints(peer int32) {
	if m.store == nil {
		return
	}
	defer common.RecoverClose("MetadataRPC.ReplayHints", nil) //nolint:staticcheck // no resource to close; log-only guard
	hints, err := m.store.ListMetadataHints(peer)
	if err != nil {
		log.Printf("AUDIT: metadata hint replay list failed for peer %d: %v (errno=%d)", peer, err, syscall.EIO)
		return
	}
	replayed, failed := 0, 0
	for _, hint := range hints {
		if len(hint.Payload) == 0 {
			continue
		}
		if err := m.replayOneHint(peer, hint.Name, hint.Payload); err != nil {
			failed++
			continue
		}
		if derr := m.store.DeleteMetadataHint(peer, hint.Name); derr != nil {
			log.Printf("AUDIT: metadata hint delete failed for peer %d name %q: %v (errno=%d)", peer, hint.Name, derr, syscall.EIO)
		}
		replayed++
	}
	if replayed > 0 || failed > 0 {
		log.Printf("AUDIT: metadata hints replayed to peer %d: %d ok, %d failed", peer, replayed, failed)
	}
}

// replayOneHint decodes and re-sends one stored record.
func (m *MetadataRPCProvider) replayOneHint(peer int32, name string, payload []byte) (err error) {
	defer common.RecoverErrWith("MetadataRPC.replayHint", syscall.EIO, &err)
	switch payload[0] {
	case metadataHintKindReplicate:
		args, derr := DecodeReplicateMetadataArgs(payload[1:])
		if derr != nil {
			return derr
		}
		_, err = m.replicateToPeer(peer, args)
		return err
	case metadataHintKindDelete:
		args, derr := DecodeDeleteMetadataArgs(payload[1:])
		if derr != nil {
			return derr
		}
		return m.deleteToPeer(peer, args)
	default:
		return syscall.EINVAL
	}
}
