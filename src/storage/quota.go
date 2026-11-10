package storage

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"syscall"

	bolt "go.etcd.io/bbolt"

	"github.com/alsotoes/momo/src/common"
)

// tenantUsage is the per-tenant accounting record. It counts logical objects
// (name + tenant), not content-addressed blobs, so two names that deduplicate to
// one blob still consume two objects (R8 quotas, #1315).
type tenantUsage struct {
	Bytes   int64
	Objects int64
}

const tenantUsageRecordSize = 16 // 8 bytes bytes + 8 bytes objects, big endian

// ErrQuotaExceeded is returned when a write would exceed a tenant quota. It wraps
// syscall.EDQUOT so surfaces can map it to their own refusal (S3: 403
// QuotaExceeded) while staying distinguishable with errors.Is.
var ErrQuotaExceeded = fmt.Errorf("tenant quota exceeded: %w", syscall.EDQUOT)

// TenantUsage reports the bytes and object count currently charged to a tenant.
// An unknown tenant reports zero.
func (s *CASStore) TenantUsage(tenantID string) (bytesUsed, objectsUsed int64, err error) {
	if tenantID == "" {
		return 0, 0, nil
	}
	err = s.db.View(func(tx *bolt.Tx) error {
		u := readTenantUsage(tx, tenantID)
		bytesUsed, objectsUsed = u.Bytes, u.Objects
		return nil
	})
	return bytesUsed, objectsUsed, err
}

// AdjustTenantQuota atomically applies a signed delta to a tenant's usage and
// rejects the change when it would exceed a configured limit. The check and the
// update happen in one transaction, so concurrent writes at the boundary cannot
// overshoot.
//
// It is a no-op (nil) when the tenant is empty, unknown, or has both quotas
// unset, which keeps native/peer writes and single-tenant deployments unchanged.
func (s *CASStore) AdjustTenantQuota(tenantID string, deltaBytes, deltaObjects int64) error {
	if tenantID == "" || (deltaBytes == 0 && deltaObjects == 0) {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		cfg := readTenantConfigTx(tx, tenantID)
		if cfg == nil || (cfg.QuotaBytes <= 0 && cfg.QuotaObjects <= 0) {
			return nil
		}
		u := applyUsageDelta(readTenantUsage(tx, tenantID), deltaBytes, deltaObjects)
		if exceedsQuota(cfg, u) {
			return ErrQuotaExceeded
		}
		return writeTenantUsage(tx, tenantID, u)
	})
}

// applyUsageDelta adds a signed delta, clamping at zero so releases can never
// drive usage negative.
func applyUsageDelta(u tenantUsage, deltaBytes, deltaObjects int64) tenantUsage {
	u.Bytes += deltaBytes
	u.Objects += deltaObjects
	if u.Bytes < 0 {
		u.Bytes = 0
	}
	if u.Objects < 0 {
		u.Objects = 0
	}
	return u
}

// exceedsQuota reports whether the projected usage is past a configured limit.
// A zero limit means unlimited on that dimension.
func exceedsQuota(cfg *common.TenantConfig, u tenantUsage) bool {
	if cfg.QuotaBytes > 0 && u.Bytes > cfg.QuotaBytes {
		return true
	}
	return cfg.QuotaObjects > 0 && u.Objects > cfg.QuotaObjects
}

// readTenantUsage reads a tenant's usage inside an existing transaction.
func readTenantUsage(tx *bolt.Tx, tenantID string) tenantUsage {
	b := tx.Bucket(bucketTenantUsage)
	if b == nil {
		return tenantUsage{}
	}
	raw := b.Get([]byte(tenantID))
	if len(raw) != tenantUsageRecordSize {
		return tenantUsage{}
	}
	return tenantUsage{
		Bytes:   int64(binary.BigEndian.Uint64(raw[0:8])),
		Objects: int64(binary.BigEndian.Uint64(raw[8:16])),
	}
}

// writeTenantUsage persists a tenant's usage inside an existing transaction.
func writeTenantUsage(tx *bolt.Tx, tenantID string, u tenantUsage) error {
	b := tx.Bucket(bucketTenantUsage)
	if b == nil {
		return fmt.Errorf("tenant_usage bucket missing: %w", syscall.EIO)
	}
	var raw [tenantUsageRecordSize]byte
	binary.BigEndian.PutUint64(raw[0:8], uint64(u.Bytes))
	binary.BigEndian.PutUint64(raw[8:16], uint64(u.Objects))
	return b.Put([]byte(tenantID), raw[:])
}

// readTenantConfigTx reads a tenant config inside an existing transaction.
func readTenantConfigTx(tx *bolt.Tx, tenantID string) *common.TenantConfig {
	b := tx.Bucket(bucketTenantMeta)
	if b == nil {
		return nil
	}
	raw := b.Get([]byte(tenantID))
	if raw == nil {
		return nil
	}
	var cfg common.TenantConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}
	return &cfg
}

// IsQuotaExceeded reports whether err is a quota refusal.
func IsQuotaExceeded(err error) bool {
	return errors.Is(err, syscall.EDQUOT)
}
