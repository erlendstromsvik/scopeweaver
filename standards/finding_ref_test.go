package standards

import "testing"

// DB-free table for ResolveFindingRef: the catalog check shared by the server
// refs API and the agent's report_finding (roadmap E.2b-3). Scheme names are
// the finding_refs scheme literals, not the catalog's own scheme ids.
func TestResolveFindingRef(t *testing.T) {
	for _, tt := range []struct {
		name    string
		scheme  string
		ref     string
		wantErr bool
	}{
		{"asvs hit", "asvs", "V1.1.1", false},
		{"asvs miss", "asvs", "V99.99.99", true},
		{"wstg hit", "wstg", "WSTG-INFO-01", false},
		{"wstg miss", "wstg", "WSTG-NOPE-99", true},
		{"top10 hit", "owasp-top10", "A01:2025", false},
		{"top10 miss", "owasp-top10", "A99:2025", true},
		{"cwe shape-only", "cwe", "CWE-79", false},
		{"cvss shape-only", "cvss", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", false},
		{"unknown scheme", "nope", "whatever", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := ResolveFindingRef(tt.scheme, tt.ref)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveFindingRef(%q, %q) err=%v, wantErr=%v", tt.scheme, tt.ref, err, tt.wantErr)
			}
		})
	}
}
