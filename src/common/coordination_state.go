package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// CoordinationModeStore persists the last replication mode a node observed, so
// a restarted or newly-elected controller resumes from the cluster's mode
// instead of resetting it to the first entry of the configured order
// (coordinator failover Phase 4: state continuity without a new RPC — every
// node already receives the mode broadcast, so each node records what it saw).
type CoordinationModeStore struct {
	path string
}

// coordinationModeFile is the file name inside <data>/coordination.
const coordinationModeFile = "replication_mode"

// NewCoordinationModeStore builds a store under the node's data directory.
// An empty dataDir yields a disabled store whose methods are no-ops.
func NewCoordinationModeStore(dataDir string) *CoordinationModeStore {
	if strings.TrimSpace(dataDir) == "" {
		return nil
	}
	return &CoordinationModeStore{path: filepath.Join(dataDir, "coordination", coordinationModeFile)}
}

// Path returns the file backing the store, or "" when disabled.
func (s *CoordinationModeStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// LoadMode returns the persisted mode and whether a value was present.
func (s *CoordinationModeStore) LoadMode() (int, bool, error) {
	if s == nil {
		return 0, false, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("read coordination mode: %w", err)
	}
	mode, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false, fmt.Errorf("parse coordination mode %q: %w", string(data), syscall.EINVAL)
	}
	return mode, true, nil
}

// SaveMode persists the mode atomically (write + fsync + rename), so a crash
// mid-write cannot leave a truncated value behind.
func (s *CoordinationModeStore) SaveMode(mode int) (err error) {
	if s == nil {
		return nil
	}
	defer RecoverErr("CoordinationModeStore.SaveMode", &err)

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create coordination state dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, coordinationModeFile+".tmp*")
	if err != nil {
		return fmt.Errorf("create coordination mode temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // no-op after a successful rename
	}()

	if _, err := tmp.WriteString(strconv.Itoa(mode) + "\n"); err != nil {
		return fmt.Errorf("write coordination mode: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync coordination mode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close coordination mode: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("rename coordination mode: %w", err)
	}
	return nil
}
