package advisory

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// vendoredKEV is a FIXED copy of the KEV catalog (2026.10.14, 1745
// entries), not the live snapshot: the advisory sync rewrites
// standards/sources/cisa-kev/known_exploited_vulnerabilities.json, so
// tests that assert exact versions/counts must read the fixture.
const vendoredKEV = "testdata/kev-2026.10.14.json"

// liveKEV is the snapshot the host-side sync actually refreshes.
const liveKEV = "../standards/sources/cisa-kev/known_exploited_vulnerabilities.json"

func TestParseKEVVendored(t *testing.T) {
	b, err := os.ReadFile(vendoredKEV)
	if err != nil {
		t.Fatalf("read vendored KEV: %v", err)
	}
	// The count field is the source of truth for the expected length.
	var meta struct {
		CatalogVersion string `json:"catalogVersion"`
		Count          int    `json:"count"`
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatalf("read meta: %v", err)
	}
	version, recs, err := ParseKEV(b)
	if err != nil {
		t.Fatalf("ParseKEV: %v", err)
	}
	if version != "2026.10.14" {
		t.Fatalf("catalogVersion = %q, want 2026.10.14", version)
	}
	if len(recs) != meta.Count {
		t.Fatalf("len(recs) = %d, want %d", len(recs), meta.Count)
	}
	if len(recs) == 0 {
		t.Fatal("no records")
	}
	// First record must carry the newest date (sorted desc).
	newest := recs[0].Date
	for _, r := range recs {
		if r.Date.After(newest) {
			t.Fatalf("record %s has date %v after first record %v", r.ID, r.Date, newest)
		}
	}
	for _, r := range recs {
		if !strings.HasPrefix(r.ID, "CVE-") {
			t.Fatalf("record ID %q does not start with CVE-", r.ID)
		}
		if r.Source != SourceCISAKEV {
			t.Fatalf("record %s source = %q, want %q", r.ID, r.Source, SourceCISAKEV)
		}
		if r.Link != "https://nvd.nist.gov/vuln/detail/"+r.ID {
			t.Fatalf("record %s link = %q", r.ID, r.Link)
		}
	}
}

// TestParseKEVLiveSnapshot parses the live snapshot the sync rewrites,
// with no exact version/count assertions: it must simply parse, carry a
// non-empty catalogVersion and a substantial entry count.
func TestParseKEVLiveSnapshot(t *testing.T) {
	b, err := os.ReadFile(liveKEV)
	if err != nil {
		t.Fatalf("read live KEV snapshot: %v", err)
	}
	version, recs, err := ParseKEV(b)
	if err != nil {
		t.Fatalf("ParseKEV: %v", err)
	}
	if version == "" {
		t.Fatal("catalogVersion is empty")
	}
	if len(recs) <= 1000 {
		t.Fatalf("len(recs) = %d, want > 1000", len(recs))
	}
}

// TestEmbeddedKEV checks the KEV catalog compiled into the binary parses with
// a non-empty catalog version and a substantial entry count. No exact
// numbers: the embedded snapshot is refreshed by the host-side sync, so the
// version and count change between builds.
func TestEmbeddedKEV(t *testing.T) {
	version, recs, err := EmbeddedKEV()
	if err != nil {
		t.Fatalf("EmbeddedKEV: %v", err)
	}
	if version == "" {
		t.Fatal("embedded catalogVersion is empty")
	}
	if len(recs) <= 1000 {
		t.Fatalf("embedded len(recs) = %d, want > 1000", len(recs))
	}
	// Cached: the second call returns the same backing slice.
	_, recs2, err := EmbeddedKEV()
	if err != nil {
		t.Fatalf("EmbeddedKEV (second call): %v", err)
	}
	if &recs[0] != &recs2[0] {
		t.Error("EmbeddedKEV re-parsed on the second call")
	}
}

func TestParseKEVCountMismatch(t *testing.T) {
	src := `{"catalogVersion":"2026.10.14","count":2,"vulnerabilities":[{"cveID":"CVE-2026-1","vendorProject":"V","product":"P","vulnerabilityName":"n","dateAdded":"2026-10-14"}]}`
	_, _, err := ParseKEV([]byte(src))
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected count mismatch error, got %v", err)
	}
}

func TestParseKEVMissingCveID(t *testing.T) {
	src := `{"catalogVersion":"2026.10.14","count":1,"vulnerabilities":[{"vendorProject":"V","product":"P","vulnerabilityName":"n","dateAdded":"2026-10-14"}]}`
	_, _, err := ParseKEV([]byte(src))
	if err == nil || !strings.Contains(err.Error(), "missing cveID") {
		t.Fatalf("expected missing cveID error, got %v", err)
	}
}

func TestParseKEVInvalidJSON(t *testing.T) {
	_, _, err := ParseKEV([]byte("{not json"))
	if err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestParseKEVRansomwareAndLink(t *testing.T) {
	src := `{"catalogVersion":"2026.10.14","count":1,"vulnerabilities":[{"cveID":"CVE-2026-9","vendorProject":"Acme","product":"Widget","vulnerabilityName":"Widget RCE","dateAdded":"2026-10-01","shortDescription":"boom","knownRansomwareCampaignUse":"Known","cwes":["CWE-78"]}]}`
	_, recs, err := ParseKEV([]byte(src))
	if err != nil {
		t.Fatalf("ParseKEV: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("len(recs) = %d, want 1", len(recs))
	}
	r := recs[0]
	if !r.Ransomware {
		t.Fatal("expected Ransomware true for Known")
	}
	if r.Link != "https://nvd.nist.gov/vuln/detail/CVE-2026-9" {
		t.Fatalf("link = %q", r.Link)
	}
	if r.Title != "Widget RCE" || r.Summary != "boom" {
		t.Fatalf("title/summary = %q / %q", r.Title, r.Summary)
	}
	if !r.Date.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("date = %v, want 2026-10-01 UTC midnight", r.Date)
	}
	if len(r.CVEs) != 1 || r.CVEs[0] != "CVE-2026-9" {
		t.Fatalf("CVEs = %v", r.CVEs)
	}
	if len(r.CWEs) != 1 || r.CWEs[0] != "CWE-78" {
		t.Fatalf("CWEs = %v", r.CWEs)
	}
}

func TestParseKEVSortsByDateDescThenID(t *testing.T) {
	src := `{"catalogVersion":"2026.10.14","count":3,"vulnerabilities":[` +
		`{"cveID":"CVE-2026-5","vendorProject":"V","product":"P","vulnerabilityName":"a","dateAdded":"2026-10-01"},` +
		`{"cveID":"CVE-2026-2","vendorProject":"V","product":"P","vulnerabilityName":"b","dateAdded":"2026-10-14"},` +
		`{"cveID":"CVE-2026-1","vendorProject":"V","product":"P","vulnerabilityName":"c","dateAdded":"2026-10-14"}]}`
	_, recs, err := ParseKEV([]byte(src))
	if err != nil {
		t.Fatalf("ParseKEV: %v", err)
	}
	want := []string{"CVE-2026-1", "CVE-2026-2", "CVE-2026-5"}
	for i, w := range want {
		if recs[i].ID != w {
			t.Fatalf("recs[%d].ID = %q, want %q", i, recs[i].ID, w)
		}
	}
}
