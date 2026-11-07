package server

import (
	"context"
	"log"
	"sort"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

// rebalancePusher ships one local blob to a peer and confirms the peer holds
// it. The rebalance loop drops local bytes only after a nil return.
type rebalancePusher interface {
	Push(hash string, targetID int) error
}

// startRebalanceLoop runs the stigmergic shedding loop (R11, #1143): each tick
// it compares local disk pressure against the configured water marks and, when
// above the high mark, moves the coldest blobs to peers that already hold a
// replica of them.
func startRebalanceLoop(ctx context.Context, cs *storage.CASStore, pusher rebalancePusher, cmap *common.ClusterMap, self int, rf int, rc common.ConfigurationRebalance) {
	if !rc.Enabled {
		return
	}
	go runRebalance(ctx, cs, pusher, cmap, self, rf, rc, time.Duration(rc.Interval)*time.Second)
	log.Printf("R11: stigmergic rebalance enabled (interval=%ds, high=%.0f%%, low=%.0f%%)", rc.Interval, rc.DiskHighWater, rc.DiskLowWater)
}

// runRebalance is the ticker loop, separated so tests can use a short period.
func runRebalance(ctx context.Context, cs *storage.CASStore, pusher rebalancePusher, cmap *common.ClusterMap, self int, rf int, rc common.ConfigurationRebalance, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if moved, err := rebalanceCycle(cs, pusher, cmap, self, rf, rc); err != nil {
				log.Printf("AUDIT: rebalance cycle failed: %v", err)
			} else if moved > 0 {
				log.Printf("REBALANCE: shed %d blob(s) to peers", moved)
			}
		}
	}
}

// rebalanceCycle performs one evaluation. It returns how many blobs moved.
func rebalanceCycle(cs *storage.CASStore, pusher rebalancePusher, cmap *common.ClusterMap, self int, rf int, rc common.ConfigurationRebalance) (int, error) {
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

	moved := 0
	for _, blob := range blobs {
		if moved >= rc.MaxShedPerCycle || usedPct <= rc.DiskLowWater {
			break
		}
		target, ok := pickShedTarget(cmap, blob.Hash, self, rf)
		if !ok {
			continue
		}
		if err := pusher.Push(blob.Hash, target); err != nil {
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

// pickShedTarget returns a peer in the blob's placement that is not this node.
func pickShedTarget(cmap *common.ClusterMap, hash string, self int, rf int) (int, bool) {
	placement, err := cmap.Placement(hash, rf)
	if err != nil {
		return 0, false
	}
	for _, node := range placement {
		if node.ID != self {
			return node.ID, true
		}
	}
	return 0, false
}

// diskUsedPercent reports the local filesystem's used percentage.
func diskUsedPercent(cs *storage.CASStore) (float64, error) {
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
