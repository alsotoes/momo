package storage

import (
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
)

// DurabilityMode selects the write-durability barrier (R3, #931). An
// acknowledged write is durable only when the configured barrier has been
// crossed before the store replies success.
type DurabilityMode int

const (
	// DurabilityFSync (default) fsyncs each blob's bytes before the write is
	// acknowledged — the historical, fully-durable behavior.
	DurabilityFSync DurabilityMode = iota
	// DurabilityGroupCommit amortizes the durability cost into a batched
	// directory barrier (PERFORMANCE_SECURITY B5): one winner-driven pass
	// fsyncs the parent directory of every blob in the batch, making all their
	// atomic renames durable together, and skips the per-blob file-data fsync.
	// Blob payload blocks may still be mid-writeback; content addressing plus
	// the R2 verified-read/self-heal loop keeps the durability + verification
	// failure model sound.
	DurabilityGroupCommit
	// DurabilityNone acknowledges buffered writes without any fsync — best
	// effort and NON-durable (a process/OS crash may lose acknowledged writes).
	DurabilityNone
)

// Durability mode names accepted by [storage] durability and WithDurability.
const (
	DurabilityNameFSync       = "fsync"
	DurabilityNameGroupCommit = "group-commit"
	DurabilityNameNone        = "none"
)

// durabilityNames maps durable mode names to modes (R3-G1).
var durabilityNames = map[string]DurabilityMode{
	DurabilityNameFSync:       DurabilityFSync,
	DurabilityNameGroupCommit: DurabilityGroupCommit,
	DurabilityNameNone:        DurabilityNone,
}

// String implements fmt.Stringer.
func (m DurabilityMode) String() string {
	switch m {
	case DurabilityFSync:
		return DurabilityNameFSync
	case DurabilityGroupCommit:
		return DurabilityNameGroupCommit
	case DurabilityNone:
		return DurabilityNameNone
	default:
		return fmt.Sprintf("DurabilityMode(%d)", int(m))
	}
}

// ParseDurabilityMode maps a config string to a mode. An empty value falls
// back to fsync; unknown values are a configuration error (syscall.EINVAL).
func ParseDurabilityMode(s string) (DurabilityMode, error) {
	if s == "" {
		return DurabilityFSync, nil
	}
	m, ok := durabilityNames[s]
	if !ok {
		return 0, fmt.Errorf("invalid durability %q (valid: %s, %s, %s): %w",
			s, DurabilityNameFSync, DurabilityNameGroupCommit, DurabilityNameNone, syscall.EINVAL)
	}
	return m, nil
}

// DurabilityOps is implemented by blob backends that can force durability at
// the R3 seam. LocalBlobStore implements both methods; remote/raw backends
// that lack fsync primitives omit it and fall back to their backend's own
// persistence guarantee (a no-op barrier).
type DurabilityOps interface {
	// SyncBlob fsyncs the file backing blob hash (per-blob data barrier).
	SyncBlob(hash string) error
	// SyncDir fsyncs the parent directory of blob hash, making its rename
	// durable (the group-commit batch barrier).
	SyncDir(hash string) error
}

// DurabilityBarrier is the Rule 74 compile-time seam (R3-C1, #931) that a
// CASStore uses to make a blob durable before acknowledging its write. It is
// selected declaratively by name via WithDurability and operates on the
// configured blob backend through DurabilityOps.
type DurabilityBarrier interface {
	// Commit makes blob hash durable before the write is acknowledged. It must
	// return an error if durability cannot be established so the write fails
	// instead of silently acknowledging.
	Commit(hash string) error
}

// fsyncBarrier fsyncs each blob's file before ack (the durable default).
type fsyncBarrier struct {
	ops DurabilityOps
}

// Commit fsyncs the blob's file before acknowledging the write (durable default).
func (b *fsyncBarrier) Commit(hash string) error {
	if b.ops == nil {
		return nil // backend persistence is the bar
	}
	if err := b.ops.SyncBlob(hash); err != nil {
		return fmt.Errorf("durability fsync failed for %s: %w", hash, err)
	}
	return nil
}

// noDurabilityBarrier acknowledges buffered writes without any fsync (none).
// Explicitly non-durable: a crash may lose acknowledged writes.
type noDurabilityBarrier struct{}

// Commit acknowledges buffered writes without fsync (non-durable mode).
func (noDurabilityBarrier) Commit(string) error { return nil }

// groupCommitBarrier covers a batch of renamed blobs with a single winner-
// driven pass of directory fsyncs. Concurrent Commit()s join the in-flight
// batch; exactly one flushes the parent directories of every pending blob,
// so N blobs pay one batch barrier instead of N per-blob barriers. Per-blob
// file-data fsync is skipped: content addressing + the R2 verified-read /
// self-heal loop covers any torn block between the barrier and OS writeback.
type groupCommitBarrier struct {
	ops      DurabilityOps
	mu       sync.Mutex
	pending  map[string]struct{}
	flushing atomic.Bool
}

func newGroupCommitBarrier(ops DurabilityOps) *groupCommitBarrier {
	return &groupCommitBarrier{ops: ops, pending: make(map[string]struct{})}
}

// Commit records hash into the batch; the first caller flushes the batch's
// parent directories once (amortized), then clears it for the next batch.
func (g *groupCommitBarrier) Commit(hash string) error {
	if g.ops == nil {
		return nil
	}
	winner := g.record(hash)
	if !winner {
		return nil // an in-flight flush covers this hash
	}
	// SyncDir for every hash that joined so far, in one pass.
	for _, h := range g.drain() {
		if err := g.ops.SyncDir(h); err != nil {
			return fmt.Errorf("durability group-commit barrier failed for %s: %w", h, err)
		}
	}
	return nil
}

// record adds hash to the pending batch. Reports whether the caller won the
// flush race for the current batch (first to see no flush in flight).
func (g *groupCommitBarrier) record(hash string) (winner bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pending[hash] = struct{}{}
	winner = !g.flushing.Swap(true)
	return winner
}

// drain returns and clears the current batch and releases the flush flag so
// the next batch can flush.
func (g *groupCommitBarrier) drain() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	batch := make([]string, 0, len(g.pending))
	for h := range g.pending {
		batch = append(batch, h)
	}
	g.pending = make(map[string]struct{})
	g.flushing.Store(false)
	return batch
}

var _ DurabilityBarrier = (*fsyncBarrier)(nil)
var _ DurabilityBarrier = (*noDurabilityBarrier)(nil)
var _ DurabilityBarrier = (*groupCommitBarrier)(nil)

// durabilityRegistry is the compiled-in Rule 74 registry of durability
// constructors, selected declaratively by name (mirrors verifierRegistry).
var durabilityRegistry = map[string]func(s *CASStore) DurabilityBarrier{
	DurabilityNameFSync: func(s *CASStore) DurabilityBarrier {
		ops, _ := s.blobs.(DurabilityOps)
		return &fsyncBarrier{ops: ops}
	},
	DurabilityNameGroupCommit: func(s *CASStore) DurabilityBarrier {
		ops, _ := s.blobs.(DurabilityOps)
		return newGroupCommitBarrier(ops)
	},
	DurabilityNameNone: func(*CASStore) DurabilityBarrier {
		return &noDurabilityBarrier{}
	},
}

// WithDurability selects the write-durability barrier for newCASStore by name
// from the compiled-in registry (R3-G1, #931). Unknown names panic at
// construction time (fail-closed) so writes are never silently under-durable.
func WithDurability(name string) func(*CASStore) {
	return func(s *CASStore) {
		ctor, ok := durabilityRegistry[name]
		if !ok {
			panic(fmt.Errorf("WithDurability: unknown durability %q: %w", name, syscall.EINVAL))
		}
		s.durability = ctor(s)
	}
}
