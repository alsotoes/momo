package storage

import (
	"errors"
	"syscall"
	"testing"

	"github.com/alsotoes/momo/src/common"
)

// TestListBucketACLs covers the enumeration the tenancy bootstrap prunes with
// (#1325): bucket entries only, object entries must not leak in.
func TestListBucketACLs(t *testing.T) {
	s, err := NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	defer func() { _ = s.Close() }()

	if acls, lerr := s.ListBucketACLs(); lerr != nil || len(acls) != 0 {
		t.Fatalf("empty store: %v %v", acls, lerr)
	}

	if err := s.PutBucketACL(&common.BucketACL{Bucket: "photos", Entries: []common.ACLEntry{{TenantID: "acme", Permission: common.PermRead}}}); err != nil {
		t.Fatalf("PutBucketACL: %v", err)
	}
	if err := s.PutBucketACL(&common.BucketACL{Bucket: "docs", Entries: []common.ACLEntry{{TenantID: "beta", Permission: common.PermWrite}}}); err != nil {
		t.Fatalf("PutBucketACL: %v", err)
	}
	if err := s.PutObjectACL(&common.ObjectACL{Key: "photos/a.txt", Entries: []common.ACLEntry{{TenantID: "acme", Permission: common.PermRead}}}); err != nil {
		t.Fatalf("PutObjectACL: %v", err)
	}

	acls, err := s.ListBucketACLs()
	if err != nil {
		t.Fatalf("ListBucketACLs: %v", err)
	}
	if len(acls) != 2 {
		t.Fatalf("expected 2 bucket ACLs (object ACLs excluded), got %d", len(acls))
	}
	seen := map[string]bool{}
	for _, a := range acls {
		seen[a.Bucket] = true
	}
	if !seen["photos"] || !seen["docs"] {
		t.Fatalf("unexpected buckets: %v", seen)
	}
}

// TestDeleteBucketACL returns the bucket to default-deny.
func TestDeleteBucketACL(t *testing.T) {
	s, err := NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.PutBucketACL(&common.BucketACL{Bucket: "photos"}); err != nil {
		t.Fatalf("PutBucketACL: %v", err)
	}
	if err := s.DeleteBucketACL("photos"); err != nil {
		t.Fatalf("DeleteBucketACL: %v", err)
	}
	acl, err := s.GetBucketACL("photos")
	if err != nil {
		t.Fatalf("GetBucketACL: %v", err)
	}
	if acl.Bucket != "" {
		t.Fatalf("ACL must be gone, got %+v", acl)
	}
	// Deleting a missing ACL is idempotent, not an error.
	if err := s.DeleteBucketACL("photos"); err != nil {
		t.Fatalf("second delete must be a no-op: %v", err)
	}
	if err := s.DeleteBucketACL(""); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("empty bucket must be refused with EINVAL, got %v", err)
	}
}
