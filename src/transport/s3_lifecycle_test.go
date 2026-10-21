package transport

import (
	"strings"
	"testing"

	"github.com/alsotoes/momo/src/storage"
)

func checkExpirationFilter(t *testing.T, cfg storage.LifecycleConfig) {
	t.Helper()
	if len(cfg.Rules) != 1 || cfg.Rules[0].ExpirationDays != 30 || cfg.Rules[0].Prefix != "logs/" {
		t.Fatalf("unexpected: %+v", cfg)
	}
}

func checkLegacyPrefix(t *testing.T, cfg storage.LifecycleConfig) {
	t.Helper()
	r := cfg.Rules[0]
	if r.Prefix != "data/" || r.TransitionDays != 7 || r.StorageClass != "STANDARD_IA" {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestParseLifecycleConfiguration(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
		check   func(*testing.T, storage.LifecycleConfig)
	}{
		{
			name:  "expiration_with_filter",
			body:  `<LifecycleConfiguration><Rule><ID>r1</ID><Status>Enabled</Status><Filter><Prefix>logs/</Prefix></Filter><Expiration><Days>30</Days></Expiration></Rule></LifecycleConfiguration>`,
			check: checkExpirationFilter,
		},
		{
			name:  "legacy_top_level_prefix",
			body:  `<LifecycleConfiguration><Rule><ID>r1</ID><Status>Enabled</Status><Prefix>data/</Prefix><Transition><Days>7</Days><StorageClass>STANDARD_IA</StorageClass></Transition></Rule></LifecycleConfiguration>`,
			check: checkLegacyPrefix,
		},
		{name: "no_rules", body: `<LifecycleConfiguration></LifecycleConfiguration>`, wantErr: true},
		{name: "missing_status", body: `<LifecycleConfiguration><Rule><ID>r</ID><Expiration><Days>1</Days></Expiration></Rule></LifecycleConfiguration>`, wantErr: true},
		{name: "invalid_status", body: `<LifecycleConfiguration><Rule><ID>r</ID><Status>Bogus</Status><Expiration><Days>1</Days></Expiration></Rule></LifecycleConfiguration>`, wantErr: true},
		{name: "no_action", body: `<LifecycleConfiguration><Rule><ID>r</ID><Status>Enabled</Status></Rule></LifecycleConfiguration>`, wantErr: true},
		{name: "expiration_zero_days", body: `<LifecycleConfiguration><Rule><ID>r</ID><Status>Enabled</Status><Expiration><Days>0</Days></Expiration></Rule></LifecycleConfiguration>`, wantErr: true},
		{name: "transition_no_class", body: `<LifecycleConfiguration><Rule><ID>r</ID><Status>Enabled</Status><Transition><Days>3</Days></Transition></Rule></LifecycleConfiguration>`, wantErr: true},
		{name: "malformed_xml", body: `<LifecycleConfiguration><Rule>`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ParseLifecycleConfiguration([]byte(tc.body))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got cfg=%+v", cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}

func TestFormatLifecycleConfigurationXMLRoundTrip(t *testing.T) {
	in := storage.LifecycleConfig{Rules: []storage.LifecycleRule{
		{ID: "expire", Status: storage.LifecycleEnabled, Prefix: "logs/", ExpirationDays: 30},
		{ID: "archive", Status: storage.LifecycleEnabled, TransitionDays: 7, StorageClass: "GLACIER"},
	}}
	out, err := ParseLifecycleConfiguration(FormatLifecycleConfigurationXML(in))
	if err != nil {
		t.Fatalf("round-trip parse: %v", err)
	}
	if len(out.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %+v", out)
	}
	if out.Rules[0].ExpirationDays != 30 || out.Rules[1].StorageClass != "GLACIER" {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

func TestS3Lifecycle_FullStackFlow(t *testing.T) {
	store, _ := newVersionedS3Stack(t)

	put := `<LifecycleConfiguration><Rule><ID>expire-logs</ID><Status>Enabled</Status><Filter><Prefix>logs/</Prefix></Filter><Expiration><Days>30</Days></Expiration></Rule></LifecycleConfiguration>`
	resp, err := runS3RequestCaptureBucket(t, s3Req("PUT", "/bucket?lifecycle", put), store, "bucket")
	if err != nil || !strings.Contains(resp, "HTTP/1.1 200 OK") {
		t.Fatalf("PUT lifecycle failed: resp=%s err=%v", resp, err)
	}

	resp, err = runS3RequestCaptureBucket(t, s3Req("GET", "/bucket?lifecycle", ""), store, "bucket")
	if err != nil && err != ErrRequestHandled {
		t.Fatalf("GET lifecycle: resp=%s err=%v", resp, err)
	}
	if !strings.Contains(resp, "<Expiration><Days>30</Days></Expiration>") || !strings.Contains(resp, "<Prefix>logs/</Prefix>") {
		t.Fatalf("GET lifecycle body missing rule:\n%s", resp)
	}

	resp, err = runS3RequestCaptureBucket(t, s3Req("DELETE", "/bucket?lifecycle", ""), store, "bucket")
	if err != nil && err != ErrRequestHandled {
		t.Fatalf("DELETE lifecycle: resp=%s err=%v", resp, err)
	}
	if !strings.Contains(resp, "204 No Content") {
		t.Fatalf("expected 204 on DELETE lifecycle:\n%s", resp)
	}

	// After deletion the config is gone.
	resp, _ = runS3RequestCaptureBucket(t, s3Req("GET", "/bucket?lifecycle", ""), store, "bucket")
	if !strings.Contains(resp, "NoSuchLifecycleConfiguration") {
		t.Fatalf("expected 404 after delete:\n%s", resp)
	}
}

func TestS3Lifecycle_GetMissingConfig(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	resp, _ := runS3RequestCaptureBucket(t, s3Req("GET", "/bucket?lifecycle", ""), store, "bucket")
	if !strings.Contains(resp, "NoSuchLifecycleConfiguration") {
		t.Fatalf("expected NoSuchLifecycleConfiguration:\n%s", resp)
	}
}

func TestS3Lifecycle_PutMalformed(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	resp, _ := runS3RequestCaptureBucket(t, s3Req("PUT", "/bucket?lifecycle", `<LifecycleConfiguration></LifecycleConfiguration>`), store, "bucket")
	if !strings.Contains(resp, "MalformedXML") {
		t.Fatalf("expected MalformedXML:\n%s", resp)
	}
}

func TestS3Lifecycle_UnsupportedStore(t *testing.T) {
	// mockStore has no lifecycle capability: every lifecycle route must 501.
	for _, req := range []string{
		s3Req("GET", "/bucket?lifecycle", ""),
		s3Req("PUT", "/bucket?lifecycle", `<LifecycleConfiguration><Rule><ID>r</ID><Status>Enabled</Status><Expiration><Days>1</Days></Expiration></Rule></LifecycleConfiguration>`),
		s3Req("DELETE", "/bucket?lifecycle", ""),
	} {
		resp, _ := runS3RequestCaptureBucket(t, req, &mockStore{}, "bucket")
		if !strings.Contains(resp, "501") || !strings.Contains(resp, "NotImplemented") {
			t.Fatalf("expected 501 NotImplemented for %q:\n%s", req[:12], resp)
		}
	}
}
