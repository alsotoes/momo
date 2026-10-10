package storage

import (
	"testing"

	"github.com/alsotoes/momo/src/common"
)

func TestTenantCRUD(t *testing.T) {
	s := newTestStore(t)

	for _, tn := range []*common.TenantConfig{
		{ID: "acme", MasterKeyID: "root-1", AuthToken: "token-acme", QuotaBytes: 100, QuotaObjects: 5, Enabled: true},
		{ID: "globex", MasterKeyID: "root-1", AuthToken: "token-globex", Enabled: true},
	} {
		if err := s.PutTenantConfig(tn); err != nil {
			t.Fatalf("PutTenantConfig(%s): %v", tn.ID, err)
		}
	}

	got, err := s.GetTenantConfig("acme")
	if err != nil || got.ID != "acme" || got.AuthToken != "token-acme" || got.QuotaObjects != 5 {
		t.Fatalf("GetTenantConfig = %+v err=%v", got, err)
	}

	list, err := s.ListTenantConfigs()
	if err != nil || len(list) != 2 {
		t.Fatalf("ListTenantConfigs len=%d err=%v", len(list), err)
	}

	if err := s.DeleteTenantConfig("acme"); err != nil {
		t.Fatalf("DeleteTenantConfig: %v", err)
	}
	if _, err := s.GetTenantConfig("acme"); err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestGetTenantConfig_NotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.GetTenantConfig("missing"); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestGetTenantIDByAuthToken(t *testing.T) {
	s := newTestStore(t)
	if err := s.PutTenantConfig(&common.TenantConfig{ID: "acme", AuthToken: "tok-a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutTenantConfig(&common.TenantConfig{ID: "globex", AuthToken: "tok-b"}); err != nil {
		t.Fatal(err)
	}

	id, err := s.GetTenantIDByAuthToken("tok-b")
	if err != nil || id != "globex" {
		t.Fatalf("GetTenantIDByAuthToken = %q err=%v", id, err)
	}
	if _, err := s.GetTenantIDByAuthToken("nope"); err == nil {
		t.Fatal("expected not-found error for unknown token")
	}
	if _, err := s.GetTenantIDByAuthToken(""); err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestACL_ValidationErrors(t *testing.T) {
	s := newTestStore(t)
	if err := s.PutBucketACL(&common.BucketACL{}); err == nil {
		t.Fatal("expected error for empty bucket")
	}
	if err := s.PutObjectACL(&common.ObjectACL{}); err == nil {
		t.Fatal("expected error for empty object key")
	}
	if _, err := s.GetBucketACL(""); err == nil {
		t.Fatal("expected error for empty bucket")
	}
	if _, err := s.GetObjectACL(""); err == nil {
		t.Fatal("expected error for empty object key")
	}
}

func TestAuthorize_InvalidInputs(t *testing.T) {
	s := newTestStore(t)
	if err := s.Authorize("", common.PermRead, "bucket/x"); err == nil {
		t.Fatal("expected error for empty tenant")
	}
	if err := s.Authorize("acme", common.PermRead, "badformat"); err == nil {
		t.Fatal("expected error for invalid resource")
	}
	if err := s.Authorize("acme", common.PermRead, "object/onlybucket"); err == nil {
		t.Fatal("expected error for malformed object resource")
	}
}

func TestAuthorize_ObjectACLOverridesBucket(t *testing.T) {
	s := newTestStore(t)
	bucket := "b1"
	if err := s.PutBucketACL(&common.BucketACL{Bucket: bucket, Entries: []common.ACLEntry{{TenantID: "t1", Permission: common.PermWrite, Effect: "allow"}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutObjectACL(&common.ObjectACL{Key: "b1/obj", Entries: []common.ACLEntry{{TenantID: "t1", Permission: common.PermRead, Effect: "allow"}}}); err != nil {
		t.Fatal(err)
	}

	// Object ACL grants read on the object; bucket ACL grants write.
	if err := s.Authorize("t1", common.PermRead, "object/b1/obj"); err != nil {
		t.Fatalf("expected object ACL read allow: %v", err)
	}
	// Write is not in the object ACL but is in the bucket ACL.
	if err := s.Authorize("t1", common.PermWrite, "object/b1/obj"); err != nil {
		t.Fatalf("expected bucket ACL write fallback: %v", err)
	}
	// Delete is in neither -> default deny.
	if err := s.Authorize("t1", common.PermDelete, "object/b1/obj"); err == nil {
		t.Fatal("expected default-deny for delete")
	}
}

func TestGetAuditLogEntries_LimitAndRange(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 5; i++ {
		if err := s.WriteAuditLog(&common.AuditLogEntry{Operation: "Op", Resource: "r", Outcome: "success"}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := s.GetAuditLogEntries(0, 1<<63-1, 2)
	if err != nil || len(entries) != 2 {
		t.Fatalf("limit query: len=%d err=%v", len(entries), err)
	}
	entries, err = s.GetAuditLogEntries(0, 1<<63-1, 0)
	if err != nil || len(entries) != 5 {
		t.Fatalf("unbounded query: len=%d err=%v", len(entries), err)
	}
}
