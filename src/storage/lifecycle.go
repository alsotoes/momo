package storage

import (
	"encoding/json"
	"fmt"
	"strings"
	"syscall"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/alsotoes/momo/src/common"
)

// Lifecycle rule status values.
const (
	LifecycleEnabled  = "Enabled"
	LifecycleDisabled = "Disabled"
)

// LifecycleRule is one S3 lifecycle rule. Only the two actions momo supports
// are modelled: Expiration (delete) and Transition (storage-class change).
// A rule with neither action is inert.
type LifecycleRule struct {
	ID             string `json:"id"`
	Status         string `json:"status"` // Enabled | Disabled
	Prefix         string `json:"prefix"`
	ExpirationDays int    `json:"expiration_days,omitempty"`
	TransitionDays int    `json:"transition_days,omitempty"`
	StorageClass   string `json:"storage_class,omitempty"`
}

// LifecycleConfig is the ordered rule set for one bucket.
type LifecycleConfig struct {
	Rules []LifecycleRule `json:"rules"`
}

// LifecycleOutcome reports what a single EvaluateLifecycle pass did.
type LifecycleOutcome struct {
	Expired      int
	Transitioned int
}

// SetBucketLifecycle stores the lifecycle configuration for bucket.
func (s *CASStore) SetBucketLifecycle(bucket string, cfg LifecycleConfig) (err error) {
	defer common.RecoverErr("CASStore.SetBucketLifecycle", &err)
	if bucket == "" {
		return fmt.Errorf("lifecycle: empty bucket: %w", syscall.EINVAL)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("lifecycle: encode config: %w: %v", syscall.EIO, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketLifecycle).Put([]byte(bucket), data)
	})
}

// BucketLifecycle returns the stored lifecycle configuration for bucket and
// whether one exists.
func (s *CASStore) BucketLifecycle(bucket string) (cfg LifecycleConfig, ok bool, err error) {
	defer common.RecoverErr("CASStore.BucketLifecycle", &err)
	s.mu.RLock()
	defer s.mu.RUnlock()
	err = s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucketLifecycle).Get([]byte(bucket))
		if raw == nil {
			return nil
		}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("lifecycle: decode config for %s: %w: %v", bucket, syscall.EIO, err)
		}
		ok = true
		return nil
	})
	return cfg, ok, err
}

// DeleteBucketLifecycle removes the lifecycle configuration for bucket.
func (s *CASStore) DeleteBucketLifecycle(bucket string) (err error) {
	defer common.RecoverErr("CASStore.DeleteBucketLifecycle", &err)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketLifecycle).Delete([]byte(bucket))
	})
}

// SetStorageClass records the S3 storage class for an object in its S3 header
// metadata (the same sidecar used for Content-Type and x-amz-meta-*).
func (s *CASStore) SetStorageClass(name, class string) (err error) {
	defer common.RecoverErr("CASStore.SetStorageClass", &err)
	headers := s.GetS3Meta(name)
	if headers == nil {
		headers = map[string]string{}
	}
	if headers["x-amz-storage-class"] == class {
		return nil
	}
	headers["x-amz-storage-class"] = class
	return s.PutS3Meta(name, headers)
}

// matchingRule returns the first Enabled rule whose prefix matches name.
func matchingRule(cfg LifecycleConfig, name string) (LifecycleRule, bool) {
	for _, r := range cfg.Rules {
		if r.Status != LifecycleEnabled {
			continue
		}
		if strings.HasPrefix(name, r.Prefix) {
			return r, true
		}
	}
	return LifecycleRule{}, false
}

// EvaluateLifecycle applies bucket's lifecycle rules to its objects, returning
// how many were expired and transitioned. Objects with an unknown modification
// time are skipped (age cannot be computed). Expiration on a versioned bucket
// writes a delete marker (preserving history); on an unversioned bucket it
// deletes permanently.
func (s *CASStore) EvaluateLifecycle(bucket string, now time.Time) (out LifecycleOutcome, err error) {
	defer common.RecoverErr("CASStore.EvaluateLifecycle", &err)

	cfg, ok, err := s.BucketLifecycle(bucket)
	if err != nil || !ok || len(cfg.Rules) == 0 {
		return out, err
	}
	versioned, err := s.IsVersioned(bucket)
	if err != nil {
		return out, err
	}

	objects, err := s.List()
	if err != nil {
		return out, err
	}
	for _, obj := range objects {
		rule, matched := matchingRule(cfg, obj.Name)
		if !matched || obj.ModTime == 0 {
			continue
		}
		if err := s.applyLifecycleRule(bucket, obj, rule, versioned, now, &out); err != nil {
			return out, err
		}
	}
	return out, nil
}

// applyLifecycleRule applies the single matching rule to one object and
// records the outcome.
func (s *CASStore) applyLifecycleRule(bucket string, obj common.FileMetadata, rule LifecycleRule, versioned bool, now time.Time, out *LifecycleOutcome) error {
	ageDays := int(now.Sub(time.Unix(0, obj.ModTime)).Hours() / 24)
	switch {
	case rule.ExpirationDays > 0 && ageDays >= rule.ExpirationDays:
		if err := s.expireObject(bucket, obj.Name, versioned); err != nil {
			return err
		}
		out.Expired++
	case rule.TransitionDays > 0 && ageDays >= rule.TransitionDays && rule.StorageClass != "":
		if err := s.SetStorageClass(obj.Name, rule.StorageClass); err != nil {
			return err
		}
		out.Transitioned++
	}
	return nil
}

// expireObject applies one expiration action.
func (s *CASStore) expireObject(bucket, name string, versioned bool) error {
	if versioned {
		if _, err := s.RecordDeleteMarker(bucket, name); err != nil {
			return err
		}
	}
	return s.Delete(name)
}
