package server

import (
	"context"
	"log"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/p2p"
	"github.com/alsotoes/momo/src/storage"
)

// installVersionReplication fans locally recorded object versions out to the
// peers that hold the object's replicas (R10 follow-up, #1233). Version records
// are otherwise node-local, so version listing/get/lock enforcement would
// differ per node.
//
// Best-effort by design: failures are audited, never fatal — the local record
// is already durable, and the next write re-replicates the chain. The hook
// runs under the store lock, so the work is handed to a goroutine.
func installVersionReplication(ctx context.Context, cs *storage.CASStore, cmap *common.ClusterMap, provider *p2p.MetadataRPCProvider, self int32, rf int) {
	if cs == nil || cmap == nil || provider == nil || rf <= 1 {
		return
	}
	rep := &versionReplicator{store: cs, cmap: cmap, provider: provider, self: self, rf: rf}
	cs.SetVersionReplicationHook(func(bucket, key string, rec storage.VersionRecord) {
		go rep.fanOut(ctx, bucket, key, rec)
	})
}

// wireVersionReplication builds the metadata RPC provider and installs the
// version-replication hook during P2P bootstrap. Kept out of bootstrapP2P to
// stay within the complexity budget.
func wireVersionReplication(ctx context.Context, cfg common.Configuration, daemons []*common.Daemon, self int32, store storage.Store, transport p2p.Transport, gossip *p2p.Gossiper) {
	cs, ok := store.(*storage.CASStore)
	if !ok {
		return
	}
	ids := make([]int32, len(daemons))
	nodes := make([]*common.Node, len(daemons))
	for i, d := range daemons {
		ids[i] = int32(i)
		nodes[i] = &common.Node{ID: i, Weight: 1, Addr: d.Host, Domain: d.FailureDomain}
	}
	mFactor := cfg.Momofs.MetadataReplication
	if mFactor <= 0 {
		mFactor = cfg.Global.ReplicationFactor
	}
	provider := p2p.NewMetadataRPCProvider(self, transport, p2p.NewRing(ids, mFactor), cs)
	gossip.SetMetadataRPCProvider(provider)
	installVersionReplication(ctx, cs, &common.ClusterMap{Nodes: nodes}, provider, self, cfg.Global.ReplicationFactor)
	installQuorumMetadataPublish(cfg, provider, mFactor)
	installMetadataDeletePropagation(cs, provider, cfg.Momofs.Enabled && mFactor > 1)
	installMetadataResolver(cs, provider, cfg.Momofs.Enabled && mFactor > 1)
	// Hinted handoff (#1142): replay records owed to a peer the moment SWIM
	// reports it alive again, so a replica that missed a quorum write or delete
	// converges without waiting for the next read (read repair) or rewrite.
	gossip.OnPeerStateChange(func(peer int32, state p2p.PeerState) {
		if state == p2p.PeerStateAlive {
			provider.ReplayMetadataHints(peer)
		}
	})
}

// versionReplicator fans one version record out to an object's replica set.
type versionReplicator struct {
	store    *storage.CASStore
	cmap     *common.ClusterMap
	provider *p2p.MetadataRPCProvider
	self     int32
	rf       int
}

// versionPlacementKey co-locates version records with the blob copies: the
// same key the replication path uses. Delete markers have no hash, so they key
// on the object name.
func versionPlacementKey(bucket, key string, rec storage.VersionRecord) string {
	if rec.Hash != "" {
		return rec.Hash
	}
	return bucket + "/" + key
}

// fanOut replicates one version record. Best-effort: failures are audited,
// never fatal. Runs in its own goroutine, so it carries the Rule 37 panic
// guard — a malformed record must not take the node down.
func (v *versionReplicator) fanOut(ctx context.Context, bucket, key string, rec storage.VersionRecord) (err error) {
	defer common.RecoverErr("version replication fan-out", &err)
	status, _, err := v.store.BucketVersioning(bucket)
	if err != nil {
		log.Printf("AUDIT: version replication skipped for %s/%s: %v", common.SanitizeLog(bucket), common.SanitizeLog(key), err)
		return
	}
	placement, perr := v.cmap.Placement(versionPlacementKey(bucket, key, rec), v.rf)
	if perr != nil {
		log.Printf("AUDIT: version replication placement failed for %s/%s: %v", common.SanitizeLog(bucket), common.SanitizeLog(key), perr)
		return
	}
	for _, node := range placement {
		if int32(node.ID) == v.self || ctx.Err() != nil {
			continue
		}
		args := &p2p.ReplicateVersionArgs{
			Bucket:           bucket,
			Key:              key,
			Record:           rec,
			VersioningStatus: status,
		}
		if _, rerr := v.provider.ReplicateVersion(int32(node.ID), args); rerr != nil {
			log.Printf("AUDIT: version record replication to node %d failed for %s/%s: %v", node.ID, common.SanitizeLog(bucket), common.SanitizeLog(key), rerr)
		}
	}
	return nil
}
