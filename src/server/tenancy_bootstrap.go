package server

import (
	"fmt"
	"log"
	"sort"

	"github.com/alsotoes/momo/src/common"
)

// tenancySeeder is the slice of the store the bootstrap needs. Declared here so
// the seeding logic is testable without a full CAS store.
type tenancySeeder interface {
	PutTenantConfig(*common.TenantConfig) error
	ListTenantConfigs() ([]*common.TenantConfig, error)
	DeleteTenantConfig(string) error
	PutBucketACL(*common.BucketACL) error
	ListBucketACLs() ([]*common.BucketACL, error)
	DeleteBucketACL(string) error
}

// bootstrapTenancy makes configuration the source of truth for tenants and bucket
// ACLs (#1325).
//
// Both layers read state nothing used to write: the S3 handshake resolves a
// tenant from the tenant_meta bucket, and Authorize is default-deny, so a
// deployment could neither authenticate a configured tenant nor grant it access.
// Seeding runs before the listener opens, so the first request already sees the
// declared tenancy.
//
// Pruning is deliberately conservative: stored entries are removed only when the
// corresponding configuration declares at least one entry. An absent or empty
// section therefore never wipes state, while removing one tenant from a section
// that still declares others does revoke it on the next start.
func bootstrapTenancy(store tenancySeeder, cfg common.Configuration) error {
	if store == nil {
		return nil
	}
	if err := seedTenants(store, cfg.Tenants); err != nil {
		return err
	}
	return seedBucketACLs(store, cfg.ACLs)
}

func seedTenants(store tenancySeeder, declared map[string]*common.TenantConfig) error {
	for _, id := range sortedTenantIDs(declared) {
		if err := store.PutTenantConfig(declared[id]); err != nil {
			return fmt.Errorf("seeding tenant %q: %w", id, err)
		}
	}
	if len(declared) == 0 {
		return nil
	}
	stored, err := store.ListTenantConfigs()
	if err != nil {
		return fmt.Errorf("listing stored tenants: %w", err)
	}
	pruned := 0
	for _, t := range stored {
		if _, ok := declared[t.ID]; ok {
			continue
		}
		if err := store.DeleteTenantConfig(t.ID); err != nil {
			return fmt.Errorf("pruning tenant %q: %w", t.ID, err)
		}
		pruned++
	}
	log.Printf("R8: tenancy bootstrap seeded %d tenant(s), pruned %d", len(declared), pruned)
	return nil
}

func seedBucketACLs(store tenancySeeder, declared map[string]*common.BucketACL) error {
	for _, bucket := range sortedACLBuckets(declared) {
		if err := store.PutBucketACL(declared[bucket]); err != nil {
			return fmt.Errorf("seeding ACL for bucket %q: %w", bucket, err)
		}
	}
	if len(declared) == 0 {
		return nil
	}
	stored, err := store.ListBucketACLs()
	if err != nil {
		return fmt.Errorf("listing stored ACLs: %w", err)
	}
	pruned := 0
	for _, acl := range stored {
		if _, ok := declared[acl.Bucket]; ok {
			continue
		}
		if err := store.DeleteBucketACL(acl.Bucket); err != nil {
			return fmt.Errorf("pruning ACL for bucket %q: %w", acl.Bucket, err)
		}
		pruned++
	}
	log.Printf("R8: tenancy bootstrap applied %d bucket ACL(s), pruned %d", len(declared), pruned)
	return nil
}

func sortedTenantIDs(m map[string]*common.TenantConfig) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedACLBuckets(m map[string]*common.BucketACL) []string {
	buckets := make([]string, 0, len(m))
	for b := range m {
		buckets = append(buckets, b)
	}
	sort.Strings(buckets)
	return buckets
}
