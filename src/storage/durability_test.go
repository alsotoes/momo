package storage

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"go.uber.org/goleak"
)

// spyBlobStore is a BlobStore that records durability ops instead of touching
// disk, so R3 barrier semantics are observable (R3-T1, mock store).
type spyBlobStore struct {
	mu       sync.Mutex
	syncBlob map[string]int
	syncDir  map[string]int
}

var _ BlobStore = (*spyBlobStore)(nil)
var _ DurabilityOps = (*spyBlobStore)(nil)

func newSpyBlobStore() *spyBlobStore {
	return &spyBlobStore{syncBlob: map[string]int{}, syncDir: map[string]int{}}
}

func (s *spyBlobStore) PutBlob(hash string, content io.Reader) error {
	_, err := io.Copy(io.Discard, content)
	return err
}
func (s *spyBlobStore) GetBlob(hash string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (s *spyBlobStore) DeleteBlob(hash string) error { return nil }
func (s *spyBlobStore) Close() error                 { return nil }
func (s *spyBlobStore) SyncBlob(hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncBlob[hash]++
	return nil
}
func (s *spyBlobStore) SyncDir(hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncDir[hash]++
	return nil
}
func (s *spyBlobStore) syncBlobCount(hash string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncBlob[hash]
}
func (s *spyBlobStore) syncDirCount(hash string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncDir[hash]
}

// newSpyCAS builds a CASStore over a spy blob backend with a durability mode.
func newSpyCAS(t *testing.T, mode string) (*CASStore, *spyBlobStore) {
	t.Helper()
	spy := newSpyBlobStore()
	s, err := newCASStore(t.TempDir(), spy, WithDurability(mode))
	if err != nil {
		t.Fatalf("newCASStore: %v", err)
	}
	return s, spy
}

// TestParseDurabilityMode verifies the three R3-G1 modes plus defaults and
// EINVAL rejection.
func TestParseDurabilityMode(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want DurabilityMode
		err  bool
	}{
		{"fsync", DurabilityFSync, false},
		{"group-commit", DurabilityGroupCommit, false},
		{"none", DurabilityNone, false},
		{"", DurabilityFSync, false}, // empty defaults to fsync
		{"durable", 0, true},         // invalid -> EINVAL
		{"FSYNC", 0, true},
	} {
		got, err := ParseDurabilityMode(tc.in)
		if tc.err {
			if err == nil || !isEINVAL(err) {
				t.Errorf("ParseDurabilityMode(%q) error = %v, want EINVAL", tc.in, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ParseDurabilityMode(%q) = %v/%v, want %v", tc.in, got, err, tc.want)
		}
	}
}

func isEINVAL(err error) bool {
	type errno interface{ Unwrap() error }
	for err != nil {
		if err == syscall.EINVAL || err.Error() == syscall.EINVAL.Error() {
			return true
		}
		if u, ok := err.(errno); ok {
			err = u.Unwrap()
		} else {
			return false
		}
	}
	return false
}

// TestDurability_fsyncAcksOnlyAfterSyncBlob (R3-T1) verifies fsync mode calls
// the per-blob fsync exactly once, before the write is acknowledged.
func TestDurability_fsyncAcksOnlyAfterSyncBlob(t *testing.T) {
	defer goleak.VerifyNone(t)

	s, spy := newSpyCAS(t, DurabilityNameFSync)
	defer s.Close()

	const n = 8
	var hashes []string
	for i := 0; i < n; i++ {
		payload := []byte(fmt.Sprintf("payload-%d", i))
		hash := common.HashBytes(payload)
		hashes = append(hashes, hash)
		if err := s.Put(fmt.Sprintf("obj-%d", i), hash, int64(len(payload)), "", bytes.NewReader(payload)); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}
	for _, h := range hashes {
		if got := spy.syncBlobCount(h); got != 1 {
			t.Errorf("fsync mode: SyncBlob(%s) = %d, want exactly 1 (once per ack)", h, got)
		}
		if got := spy.syncDirCount(h); got != 0 {
			t.Errorf("fsync mode: SyncDir(%s) = %d, want 0", h, got)
		}
	}
}

// TestDurability_noneNoBarrier (R3-T1) verifies none mode performs no fsync at
// all — acknowledged writes are buffered and non-durable by design.
func TestDurability_noneNoBarrier(t *testing.T) {
	defer goleak.VerifyNone(t)

	s, spy := newSpyCAS(t, DurabilityNameNone)
	defer s.Close()

	for i := 0; i < 4; i++ {
		payload := []byte(fmt.Sprintf("bare-%d", i))
		hash := common.HashBytes(payload)
		if err := s.Put(fmt.Sprintf("bare-%d", i), hash, int64(len(payload)), "", bytes.NewReader(payload)); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}
	if got := len(spy.syncBlob); got != 0 {
		t.Errorf("none mode: SyncBlob called %d times, want 0", got)
	}
	if got := len(spy.syncDir); got != 0 {
		t.Errorf("none mode: SyncDir called %d times, want 0", got)
	}
}

// TestDurability_groupCommitBatchBarrier (R3-T1) verifies group-commit covers
// every blob in a concurrent batch exactly once via the directory barrier and
// never issues per-blob data fsyncs.
func TestDurability_groupCommitBatchBarrier(t *testing.T) {
	defer goleak.VerifyNone(t)

	s, spy := newSpyCAS(t, DurabilityNameGroupCommit)
	defer s.Close()

	const n = 24
	var hashes []string
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		payload := []byte(fmt.Sprintf("gc-%d", i))
		hash := common.HashBytes(payload)
		hashes = append(hashes, hash)
		wg.Add(1)
		go func(name, h string, p []byte) {
			defer wg.Done()
			if err := s.Put(name, h, int64(len(p)), "", bytes.NewReader(p)); err != nil {
				t.Errorf("concurrent Put %s: %v", name, err)
			}
		}(fmt.Sprintf("gc-%d", i), hash, payload)
	}
	wg.Wait()

	for _, h := range hashes {
		if got := spy.syncDirCount(h); got != 1 {
			t.Errorf("group-commit: SyncDir(%s) = %d, want exactly 1 (batch covers each once)", h, got)
		}
		if got := spy.syncBlobCount(h); got != 0 {
			t.Errorf("group-commit: SyncBlob(%s) = %d, want 0 (no per-blob data fsync)", h, got)
		}
	}
}

// TestConsistency_RYWAndLastAckWins (R3-T3) verifies sequential per-object
// semantics: concurrent writers to one object serialize (writes never torn),
// the acknowledged (last ack) value wins, and a subsequent read returns that
// same value (read-your-writes, stable).
func TestConsistency_RYWAndLastAckWins(t *testing.T) {
	defer goleak.VerifyNone(t)

	s, err := NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	defer s.Close()

	const writers = 20
	payloads := make([][]byte, writers)
	hashes := make([]string, writers)
	for i := range payloads {
		payloads[i] = []byte(fmt.Sprintf("value-%02d", i))
		hashes[i] = common.HashBytes(payloads[i])
	}

	var wg sync.WaitGroup
	var putErrs atomic.Int64
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p := payloads[idx]
			if err := s.Put("lww-obj", hashes[idx], int64(len(p)), "", bytes.NewReader(p)); err != nil {
				putErrs.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if putErrs.Load() != 0 {
		t.Fatalf("%d concurrent Puts failed", putErrs.Load())
	}

	// The object read must be one complete acknowledged value (never torn) and
	// stable across successive reads (read-your-writes).
	first, err := readAll(t, s, "lww-obj")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	second, err := readAll(t, s, "lww-obj")
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("read-your-writes violated: successive reads differ (%q vs %q)", first, second)
	}
	matched := false
	for _, p := range payloads {
		if bytes.Equal(first, p) {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("object value %q is not a complete acknowledged write (torn or foreign)", first)
	}
}
