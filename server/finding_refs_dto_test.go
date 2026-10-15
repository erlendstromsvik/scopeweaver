package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// TestFindingFromDBMapsRefs checks the DTO carries the finding's classification
// refs, and that a finding without refs serializes with no "refs" key.
func TestFindingFromDBMapsRefs(t *testing.T) {
	f := &db.DBFinding{
		ID: 7, VulnClass: "xss", Name: "Reflected XSS", Severity: "high",
		Status: db.FindingConfirmed,
		Refs: []*db.FindingRef{
			{FindingID: 7, Scheme: db.SchemeCWE, Ref: "CWE-79"},
			{FindingID: 7, Scheme: db.SchemeOWASPTop10, Ref: "A05:2025"},
		},
	}
	dto := findingFromDB(f, nil)
	if len(dto.Refs) != 2 {
		t.Fatalf("Refs = %d, want 2", len(dto.Refs))
	}
	if dto.Refs[0] != (FindingRefDTO{Scheme: "cwe", Ref: "CWE-79"}) {
		t.Errorf("Refs[0] = %+v", dto.Refs[0])
	}
	if dto.Refs[1] != (FindingRefDTO{Scheme: "owasp-top10", Ref: "A05:2025"}) {
		t.Errorf("Refs[1] = %+v", dto.Refs[1])
	}
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"refs":[{"scheme":"cwe","ref":"CWE-79"},{"scheme":"owasp-top10","ref":"A05:2025"}]`) {
		t.Errorf("JSON missing refs array: %s", b)
	}

	// No refs -> Refs nil and no "refs" key in JSON.
	bare := findingFromDB(&db.DBFinding{ID: 8, VulnClass: "info", Severity: "low"}, nil)
	if bare.Refs != nil {
		t.Errorf("Refs = %+v, want nil for a finding without refs", bare.Refs)
	}
	b2, err := json.Marshal(bare)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b2), `"refs"`) {
		t.Errorf("JSON should omit refs key, got: %s", b2)
	}
}
