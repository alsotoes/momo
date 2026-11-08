package server

import (
	"context"
	"log"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/p2p"
	"github.com/alsotoes/momo/src/storage"
)

// rebalancePusher ships one local blob to a peer and confirms the peer holds
// it. The rebalance loop drops local bytes only after a nil return.
type rebalancePusher interface {
	Push(ctx context.Context, hash string, targetID int) error
}

// startRebalanceLoop runs the stigmergic shedding loop (R11, #1143): each tick
// it compares local disk pressure against the configured water marks and, when
// above the high mark, moves the coldest blobs to peers that already hold a
// replica of them.
func startRebalanceLoop(ctx context.Context, cs *storage.CASStore, pusher rebalancePusher, cmap *common.ClusterMap, self int, rf int, rc common.ConfigurationRebalance) {
	if !rc.Enabled {
		return
	}
	if rc.MaxBandwidthMbps <= 0 {
		log.Printf("AUDIT: rebalance is enabled with unlimited bandwidth (max_bandwidth_mbps=0); set a cap so shedding cannot starve foreground traffic")
	}
	runner := &rebalanceRunner{store: cs, pusher: pusher, cmap: cmap, self: self, rf: rf, cfg: rc}
	go runner.run(ctx, time.Duration(rc.Interval)*time.Second)
	log.Printf("R11: stigmergic rebalance enabled (interval=%ds, high=%.0f%%, low=%.0f%%)", rc.Interval, rc.DiskHighWater, rc.DiskLowWater)
}

// rebalanceRunner holds one node's shedding state.
type rebalanceRunner struct {
	// runMu serializes evaluation passes: a tick and a join migration could
	// otherwise push the same blob twice before either drop lands.
	runMu  sync.Mutex
	store  *storage.CASStore
	pusher rebalancePusher
	cmap   *common.ClusterMap
	self   int
	rf     int
	cfg    common.ConfigurationRebalance
}

// run is the ticker loop, separated so tests can use a short period.
func (r *rebalanceRunner) run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runMu.Lock()
			if c, ok := r.pusher.(interface{ IncRebalanceCycles() }); ok {
				c.IncRebalanceCycles()
			}
			moved, err := rebalanceCycle(ctx, r.store, r.pusher, r.cmap, r.self, r.rf, r.cfg)
			r.runMu.Unlock()
			if err != nil {
				log.Printf("AUDIT: rebalance cycle failed: %v", err)
			} else if moved > 0 {
				log.Printf("REBALANCE: shed %d blob(s) to peers", moved)
			}
		}
	}
}

// rebalanceCycle performs one evaluation. It returns how many blobs moved.
func rebalanceCycle(ctx context.Context, cs *storage.CASStore, pusher rebalancePusher, cmap *common.ClusterMap, self int, rf int, rc common.ConfigurationRebalance) (moved int, err error) {
	defer common.RecoverErr("rebalanceCycle", &err)
	usedPct, err := diskUsedPercent(cs)
	if err != nil {
		return 0, err
	}
	if usedPct < rc.DiskHighWater {
		return 0, nil
	}
	blobs, err := cs.ListBlobs()
	if err != nil {
		return 0, err
	}
	// Coldness proxy: oldest write first. No access counters exist yet, and
	// the oldest blob is the best available guess at "least recently useful".
	sort.Slice(blobs, func(i, j int) bool { return blobs[i].ModTime < blobs[j].ModTime })

	for _, blob := range blobs {
		if moved >= rc.MaxShedPerCycle || usedPct <= rc.DiskLowWater {
			break
		}
		target, ok := pickShedTarget(cmap, blob.Hash, self, rf)
		if !ok {
			continue
		}
		if err := pusher.Push(ctx, blob.Hash, target); err != nil {
			log.Printf("AUDIT: rebalance push of %s to node %d failed: %v", common.SanitizeLog(blob.Hash), target, err)
			continue
		}
		if err := cs.DropBlob(blob.Hash); err != nil {
			log.Printf("AUDIT: rebalance drop of %s failed: %v", common.SanitizeLog(blob.Hash), err)
			continue
		}
		moved++
		if pct, perr := diskUsedPercent(cs); perr == nil {
			usedPct = pct
		}
	}
	return moved, nil
}

// onPeerJoin migrates blobs whose placement moved to the joining peer: the
// cluster config is static, so a "join" is a configured peer becoming
// reachable, and the blobs it should hold were previously stored locally.
func (r *rebalanceRunner) onPeerJoin(ctx context.Context, peerID int) (err error) {
	defer common.RecoverErr("rebalance peer join", &err)
	r.runMu.Lock()
	defer r.runMu.Unlock()
	moved, merr := r.migrateToPeer(ctx, peerID)
	if merr != nil {
		log.Printf("AUDIT: rebalance on join of node %d failed: %v", peerID, merr)
		return merr
	}
	if moved > 0 {
		log.Printf("REBALANCE: moved %d blob(s) to joining node %d", moved, peerID)
	}
	return nil
}

// migrateToPeer pushes local blobs that the peer now owns and that this node no
// longer owns, then drops the local bytes after the peer's ack. It never
// deletes when placement still includes this node, so it cannot reduce the
// replica count.
func (r *rebalanceRunner) migrateToPeer(ctx context.Context, peerID int) (int, error) {
	blobs, err := r.store.ListBlobs()
	if err != nil {
		return 0, err
	}
	sort.Slice(blobs, func(i, j int) bool { return blobs[i].ModTime < blobs[j].ModTime })
	moved := 0
	for _, blob := range blobs {
		if moved >= r.cfg.MaxShedPerCycle || ctx.Err() != nil {
			break
		}
		if r.migrateBlob(ctx, blob.Hash, peerID) {
			moved++
		}
	}
	return moved, nil
}

// migrateBlob moves one blob when the peer owns it and this node does not.
func (r *rebalanceRunner) migrateBlob(ctx context.Context, hash string, peerID int) bool {
	peerOwns, selfOwns, err := placementOwnership(r.cmap, hash, r.rf, r.self, peerID)
	if err != nil || !peerOwns || selfOwns {
		return false
	}
	if err := r.pusher.Push(ctx, hash, peerID); err != nil {
		log.Printf("AUDIT: rebalance migration of %s to node %d failed: %v", common.SanitizeLog(hash), peerID, err)
		return false
	}
	if err := r.store.DropBlob(hash); err != nil {
		log.Printf("AUDIT: rebalance migration drop of %s failed: %v", common.SanitizeLog(hash), err)
		return false
	}
	return true
}

// placementOwnership reports whether the peer and this node own a blob.
func placementOwnership(cmap *common.ClusterMap, hash string, rf, self, peerID int) (peerOwns, selfOwns bool, err error) {
	placement, err := cmap.Placement(hash, rf)
	if err != nil {
		return false, false, err
	}
	for _, node := range placement {
		peerOwns = peerOwns || node.ID == peerID
		selfOwns = selfOwns || node.ID == self
	}
	return peerOwns, selfOwns, nil
}

var (
	peerLoadMu     sync.RWMutex
	peerLoadSource func(int32) (p2p.PeerLoad, bool)
)

// setPeerLoadSource wires the gossip load reader so shedding can avoid pushing
// bytes onto a peer that is already saturated (R11 request redirection, #1261).
// nil disables load awareness.
func setPeerLoadSource(fn func(int32) (p2p.PeerLoad, bool)) {
	peerLoadMu.Lock()
	peerLoadSource = fn
	peerLoadMu.Unlock()
}

// currentPeerLoad reads one peer's last reported load, if any.
func currentPeerLoad(id int32) (p2p.PeerLoad, bool) {
	peerLoadMu.RLock()
	fn := peerLoadSource
	peerLoadMu.RUnlock()
	if fn == nil {
		return p2p.PeerLoad{}, false
	}
	return fn(id)
}

// load tiers: a healthy known peer is preferred, an unknown peer is acceptable,
// and a saturated peer is only used when nothing else is available.
const (
	loadTierHealthy = iota
	loadTierUnknown
	loadTierSaturated
)

// peerLoadTier classifies a peer for target selection.
func peerLoadTier(id int32) (tier int, disk uint8, inflight uint32) {
	load, ok := currentPeerLoad(id)
	if !ok {
		return loadTierUnknown, 255, 0
	}
	if load.DiskUsedPercent >= 95 || load.InflightRequests >= 1000 {
		return loadTierSaturated, load.DiskUsedPercent, load.InflightRequests
	}
	return loadTierHealthy, load.DiskUsedPercent, load.InflightRequests
}

// pickShedTarget returns the least-loaded peer in the blob's placement that is
// not this node. Load awareness is best-effort: with no reports, the first
// eligible peer is used as before.
func pickShedTarget(cmap *common.ClusterMap, hash string, self int, rf int) (int, bool) {
	placement, err := cmap.Placement(hash, rf)
	if err != nil {
		return 0, false
	}
	bestID := 0
	bestTier, bestDisk, bestInflight := loadTierSaturated+1, uint8(255), ^uint32(0)
	found := false
	for _, node := range placement {
		if node.ID == self {
			continue
		}
		tier, disk, inflight := peerLoadTier(int32(node.ID))
		if !found || tier < bestTier ||
			(tier == bestTier && disk < bestDisk) ||
			(tier == bestTier && disk == bestDisk && inflight < bestInflight) {
			bestID, bestTier, bestDisk, bestInflight = node.ID, tier, disk, inflight
			found = true
		}
	}
	return bestID, found
}

// diskUsedPercent reports the local filesystem's used percentage.
func diskUsedPercent(cs *storage.CASStore) (pct float64, err error) {
	defer common.RecoverErr("diskUsedPercent", &err)
	used, free, err := cs.GetDiskUsage()
	if err != nil {
		return 0, err
	}
	total := used + free
	if total == 0 {
		return 0, syscall.EIO
	}
	return float64(used) * 100 / float64(total), nil
}
