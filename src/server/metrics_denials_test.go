package server

import (
	"strings"
	"testing"
)

// TestDenialCountersAreExported pins both refusal counters: a refusal completes
// no operation, so without these families an operator cannot tell "nobody is
// asking" from "everybody is being denied" (#1319).
func TestDenialCountersAreExported(t *testing.T) {
	mc := NewMetricsCollector()
	mc.IncAuthzDenial()
	mc.IncAuthzDenial()
	mc.IncQuotaRefusal()

	var b strings.Builder
	mc.writeMetrics(&b)
	out := b.String()

	for _, want := range []string{
		"# TYPE momo_authorization_denials_total counter",
		"momo_authorization_denials_total 2",
		"# TYPE momo_tenant_quota_refusals_total counter",
		"momo_tenant_quota_refusals_total 1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestDenialCountersStartAtZero keeps dashboards honest before the first denial.
func TestDenialCountersStartAtZero(t *testing.T) {
	mc := NewMetricsCollector()
	var b strings.Builder
	mc.writeMetrics(&b)
	out := b.String()
	if !strings.Contains(out, "momo_authorization_denials_total 0") ||
		!strings.Contains(out, "momo_tenant_quota_refusals_total 0") {
		t.Fatalf("denial counters must be declared at zero:\n%s", out)
	}
}
