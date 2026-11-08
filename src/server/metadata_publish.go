package server

import (
	"fmt"
	"log"
	"sync"
	"syscall"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/p2p"
	"github.com/alsotoes/momo/src/storage"
)

// metadataPublisher writes an object's metadata to its distributed replica set
// and reports whether a quorum acknowledged it (R6 v2 write path, #1142).
type metadataPublisher interface {
	PublishMetadata(name, hash string, size int64, remotePath string) error
}

// quorumMetadataPublisher adapts the P2P metadata provider to the write path.
type quorumMetadataPublisher struct {
	provider *p2p.MetadataRPCProvider
}

// PublishMetadata writes one record through the quorum protocol.
func (q *quorumMetadataPublisher) PublishMetadata(name, hash string, size int64, remotePath string) error {
	reply, err := q.provider.PutMetadata(&p2p.PutMetadataArgs{
		Name:       name,
		Hash:       hash,
		Size:       size,
		RemotePath: remotePath,
	})
	if err != nil {
		return err
	}
	if reply == nil || !reply.Success {
		return fmt.Errorf("metadata quorum rejected %q: %w", name, syscall.EIO)
	}
	return nil
}

var (
	metadataPublishMu       sync.RWMutex
	metadataPublisherActive metadataPublisher
)

// setMetadataPublisher wires (or clears, with nil) the quorum metadata writer.
func setMetadataPublisher(p metadataPublisher) {
	metadataPublishMu.Lock()
	metadataPublisherActive = p
	metadataPublishMu.Unlock()
}

// publishMetadataToQuorum sends the record when distributed metadata is
// enabled. Disabled (nil publisher) is a no-op so single-node deployments are
// unaffected.
func publishMetadataToQuorum(name, hash string, size int64, remotePath string) error {
	metadataPublishMu.RLock()
	p := metadataPublisherActive
	metadataPublishMu.RUnlock()
	if p == nil {
		return nil
	}
	if err := p.PublishMetadata(name, hash, size, remotePath); err != nil {
		log.Printf("AUDIT: metadata replication failed for %q: %v", name, common.SanitizeLog(err.Error()))
		return err
	}
	return nil
}

// installMetadataDeletePropagation registers the tombstone hook: after a local
// delete, the tombstone is fanned out to the metadata replica set. The hook runs
// in a goroutine (the delete already committed) and replica-side deletes use
// DeleteReplica, so propagation cannot loop.
func installMetadataDeletePropagation(cs *storage.CASStore, provider *p2p.MetadataRPCProvider, enabled bool) {
	if cs == nil || provider == nil || !enabled {
		return
	}
	cs.SetMetadataDeleteHook(func(name string) {
		go func() {
			if err := provider.DeleteMetadata(name); err != nil {
				log.Printf("AUDIT: tombstone propagation failed for %q: %v", name, common.SanitizeLog(err.Error()))
			}
		}()
	})
}

// installQuorumMetadataPublish enables quorum metadata writes when the
// deployment opts into distributed metadata (momofs enabled with a replication
// factor above one) and a provider exists.
func installQuorumMetadataPublish(cfg common.Configuration, provider *p2p.MetadataRPCProvider, mFactor int) {
	if provider == nil || !cfg.Momofs.Enabled || mFactor <= 1 {
		setMetadataPublisher(nil)
		return
	}
	setMetadataPublisher(&quorumMetadataPublisher{provider: provider})
	log.Printf("R6: quorum metadata writes enabled (replication=%d)", mFactor)
}
