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
	cs.SetVersionReplicationHook(func(bucket, key string, rec storage.VersionRecord) {
		go fanOutVersionRecord(ctx, cs, cmap, provider, self, rf, bucket, key, rec)
	})
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

// fanOutVersionRecord replicates one version record to the object's replica
// set. Best-effort: failures are audited, never fatal.
func fanOutVersionRecord(ctx context.Context, cs *storage.CASStore, cmap *common.ClusterMap, provider *p2p.MetadataRPCProvider, self int32, rf int, bucket, key string, rec storage.VersionRecord) {
	status, _, err := cs.BucketVersioning(bucket)
	if err != nil {
		log.Printf("AUDIT: version replication skipped for %s/%s: %v", common.SanitizeLog(bucket), common.SanitizeLog(key), err)
		return
	}
	placement, perr := cmap.Placement(versionPlacementKey(bucket, key, rec), rf)
	if perr != nil {
		log.Printf("AUDIT: version replication placement failed for %s/%s: %v", common.SanitizeLog(bucket), common.SanitizeLog(key), perr)
		return
	}
	for _, node := range placement {
		if int32(node.ID) == self || ctx.Err() != nil {
			continue
		}
		args := &p2p.ReplicateVersionArgs{
			Bucket:           bucket,
			Key:              key,
			Record:           rec,
			VersioningStatus: status,
		}
		if _, rerr := provider.ReplicateVersion(int32(node.ID), args); rerr != nil {
			log.Printf("AUDIT: version record replication to node %d failed for %s/%s: %v", node.ID, common.SanitizeLog(bucket), common.SanitizeLog(key), rerr)
		}
	}
}
