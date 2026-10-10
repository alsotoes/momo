package storage

import (
	"os"
	"testing"

	"github.com/alsotoes/momo/src/common"
)

func newTestStore(t *testing.T) *CASStore {
	tmpDir, err := os.MkdirTemp("", "momo-acl-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() {
		store.Close()
		os.RemoveAll(store.base)
	})
	return store
}

func TestACL_Basic(t *testing.T) {
	s := newTestStore(t)

	acl := &common.BucketACL{
		Bucket: "test-bucket",
		Entries: []common.ACLEntry{
			{TenantID: "tenant-a", Permission: common.PermRead, Effect: "allow"},
			{TenantID: "tenant-b", Permission: common.PermWrite, Effect: "allow"},
		},
	}

	if err := s.PutBucketACL(acl); err != nil {
		t.Fatalf("PutBucketACL: %v", err)
	}

	got, err := s.GetBucketACL("test-bucket")
	if err != nil {
		t.Fatalf("GetBucketACL: %v", err)
	}
	if got == nil {
		t.Fatal("expected ACL, got nil")
	}
	if got.Bucket != "test-bucket" {
		t.Fatalf("bucket name mismatch: %s", got.Bucket)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got.Entries))
	}
}

func TestACL_Object(t *testing.T) {
	s := newTestStore(t)

	acl := &common.ObjectACL{
		Key: "test-key",
		Entries: []common.ACLEntry{
			{TenantID: "tenant-a", Permission: common.PermRead, Effect: "allow"},
			{TenantID: "tenant-b", Permission: common.PermWrite, Effect: "allow"},
		},
	}

	if err := s.PutObjectACL(acl); err != nil {
		t.Fatalf("PutObjectACL: %v", err)
	}

	got, err := s.GetObjectACL("test-key")
	if err != nil {
		t.Fatalf("GetObjectACL: %v", err)
	}
	if got == nil {
		t.Fatal("expected ACL, got nil")
	}
	if got.Key != "test-key" {
		t.Fatalf("key mismatch: %s", got.Key)
	}
}

func TestAuthorize_Basic(t *testing.T) {
	s := newTestStore(t)

	acl := &common.BucketACL{
		Bucket: "test-bucket",
		Entries: []common.ACLEntry{
			{TenantID: "tenant-a", Permission: common.PermRead, Effect: "allow"},
			{TenantID: "tenant-b", Permission: common.PermWrite, Effect: "allow"},
		},
	}
	if err := s.PutBucketACL(acl); err != nil {
		t.Fatalf("PutBucketACL: %v", err)
	}

	if err := s.Authorize("tenant-a", common.PermRead, "bucket/test-bucket"); err != nil {
		t.Fatalf("tenant-a read allowed: %v", err)
	}

	if err := s.Authorize("tenant-a", common.PermWrite, "bucket/test-bucket"); err == nil {
		t.Fatal("tenant-a write should be denied")
	}

	if err := s.Authorize("tenant-b", common.PermWrite, "bucket/test-bucket"); err != nil {
		t.Fatalf("tenant-b write allowed: %v", err)
	}

	if err := s.Authorize("tenant-c", common.PermRead, "bucket/test-bucket"); err == nil {
		t.Fatal("tenant-c should be denied (no matching ACL)")
	}
}

func TestAuthorize_AdminBypass(t *testing.T) {
	s := newTestStore(t)

	if err := s.Authorize("admin", common.PermWrite, "bucket/any"); err != nil {
		t.Fatalf("admin should bypass: %v", err)
	}
	if err := s.Authorize("admin", common.PermDelete, "object/any"); err != nil {
		t.Fatalf("admin should bypass delete: %v", err)
	}
}

func TestAuthorize_ObjectACL(t *testing.T) {
	s := newTestStore(t)

	bucketACL := &common.BucketACL{
		Bucket: "test-bucket",
		Entries: []common.ACLEntry{
			{TenantID: "tenant-a", Permission: common.PermRead, Effect: "allow"},
		},
	}
	if err := s.PutBucketACL(bucketACL); err != nil {
		t.Fatalf("PutBucketACL: %v", err)
	}

	objectACL := &common.ObjectACL{
		Key: "secret.txt",
		Entries: []common.ACLEntry{
			{TenantID: "tenant-a", Permission: common.PermDelete, Effect: "deny"},
		},
	}
	if err := s.PutObjectACL(objectACL); err != nil {
		t.Fatalf("PutObjectACL: %v", err)
	}

	if err := s.Authorize("tenant-a", common.PermDelete, "object/test-bucket/secret.txt"); err == nil {
		t.Fatal("tenant-a should be denied delete on secret.txt")
	}

	if err := s.Authorize("tenant-a", common.PermRead, "object/test-bucket/secret.txt"); err != nil {
		t.Fatalf("tenant-a should be allowed read on secret.txt: %v", err)
	}
}

func TestAuthorize_DefaultDeny(t *testing.T) {
	s := newTestStore(t)

	if err := s.Authorize("tenant-a", common.PermRead, "bucket/any"); err == nil {
		t.Fatal("default deny: should deny when no ACL")
	}
}

func TestAuthorize_InvalidInput(t *testing.T) {
	s := newTestStore(t)

	if err := s.Authorize("", common.PermRead, "bucket/test"); err == nil {
		t.Fatal("empty tenant ID should error")
	}

	if err := s.Authorize("tenant-a", common.PermRead, "invalid"); err == nil {
		t.Fatal("invalid resource format should error")
	}
}
