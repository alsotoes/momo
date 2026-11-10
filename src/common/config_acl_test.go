package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeACLConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "momo.conf")
	if err := os.WriteFile(path, []byte(validConfig+body), 0666); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestGetConfig_ACLParsing covers the declarative ACL surface (#1325): a bucket
// section whose keys are tenants and whose values are permission lists.
func TestGetConfig_ACLParsing(t *testing.T) {
	cfg, err := GetConfig(writeACLConfig(t, `
[acl.photos]
acme = read,write
beta = read,!delete
* = read

[acl.docs]
acme = admin
`))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if len(cfg.ACLs) != 2 {
		t.Fatalf("expected 2 buckets with ACLs, got %d", len(cfg.ACLs))
	}
	photos := cfg.ACLs["photos"]
	if photos == nil || photos.Bucket != "photos" {
		t.Fatalf("photos ACL missing or misnamed: %+v", photos)
	}
	want := map[string]ACLEntry{
		"acme/read":   {TenantID: "acme", Permission: PermRead, Effect: "allow"},
		"acme/write":  {TenantID: "acme", Permission: PermWrite, Effect: "allow"},
		"beta/read":   {TenantID: "beta", Permission: PermRead, Effect: "allow"},
		"beta/delete": {TenantID: "beta", Permission: PermDelete, Effect: "deny"},
		"*/read":      {TenantID: "*", Permission: PermRead, Effect: "allow"},
	}
	for _, e := range photos.Entries {
		key := e.TenantID + "/" + string(e.Permission)
		expected, ok := want[key]
		if !ok {
			t.Fatalf("unexpected ACL entry %s", key)
		}
		if e.Effect != expected.Effect {
			t.Fatalf("entry %s effect = %q, want %q", key, e.Effect, expected.Effect)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing ACL entries: %v", want)
	}
	if docs := cfg.ACLs["docs"]; docs == nil || len(docs.Entries) != 1 || docs.Entries[0].Permission != PermAdmin {
		t.Fatalf("docs ACL wrong: %+v", docs)
	}
}

// TestGetConfig_ACLRejectsBadEntries keeps a typo from silently widening or
// narrowing access: an unknown permission fails the load.
func TestGetConfig_ACLRejectsBadEntries(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"unknown permission", "\n[acl.photos]\nacme = read,reed\n", "reed"},
		{"empty permission list", "\n[acl.photos]\nacme = ,\n", "no permissions"},
		{"section without bucket", "\n[acl.]\nacme = read\n", "no bucket name"},
		{"empty tenant key", "\n[acl.photos]\n\"\" = read\n", "empty key"},
		{"traversal in bucket", "\n[acl.../etc]\nacme = read\n", "invalid bucket name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := GetConfig(writeACLConfig(t, tc.body))
			if err == nil {
				t.Fatalf("expected a load error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v does not mention %q", err, tc.want)
			}
		})
	}
}

// TestGetConfig_NoACLSectionsIsNotAnError keeps the section optional.
func TestGetConfig_NoACLSectionsIsNotAnError(t *testing.T) {
	cfg, err := GetConfig(writeACLConfig(t, ""))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if len(cfg.ACLs) != 0 {
		t.Fatalf("expected no ACLs, got %d", len(cfg.ACLs))
	}
}

// TestParseACLPermissionsEdges covers the parser's tolerances: surrounding
// whitespace, empty list items, the admin permission and repeated permissions.
func TestParseACLPermissionsEdges(t *testing.T) {
	entries, err := parseACLPermissions("acme", " read , ,admin,write,, ")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (empty items skipped), got %d: %+v", len(entries), entries)
	}
	perms := map[Permission]string{}
	for _, e := range entries {
		perms[e.Permission] = e.Effect
		if e.TenantID != "acme" {
			t.Fatalf("tenant must be preserved, got %q", e.TenantID)
		}
	}
	if perms[PermRead] != "allow" || perms[PermAdmin] != "allow" || perms[PermWrite] != "allow" {
		t.Fatalf("unexpected permissions: %+v", perms)
	}

	if _, err := parseACLPermissions("acme", "  ,  "); err == nil {
		t.Fatal("a list of only separators must be refused")
	}
	if _, err := parseACLPermissions("acme", "!nonsense"); err == nil {
		t.Fatal("a deny of an unknown permission must be refused")
	}
}
