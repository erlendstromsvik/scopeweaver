package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/locale"
	"github.com/Autumn-27/artex/standards"
)

// fixtureCatalog builds the small DB-free catalog used by the coverage tests.
func fixtureCatalog() *standards.Catalog {
	return &standards.Catalog{
		ASVS: map[string]standards.ASVSRequirement{
			"V1.1.1":  {ID: "V1.1.1", Chapter: "1", Section: "1.1", Description: "Threat modeling", Level: 1},
			"V1.1.2":  {ID: "V1.1.2", Chapter: "1", Section: "1.1", Description: "Security lifecycle", Level: 2},
			"V1.2.9":  {ID: "V1.2.9", Chapter: "1", Section: "1.2", Description: "File upload checks", Level: 1},
			"V1.2.10": {ID: "V1.2.10", Chapter: "1", Section: "1.2", Description: "Backup handling", Level: 1},
			"V2.1.1":  {ID: "V2.1.1", Chapter: "2", Section: "2.1", Description: "Password strength", Level: 3},
			"V10.1.1": {ID: "V10.1.1", Chapter: "10", Section: "10.1", Description: "Malicious code review", Level: 1},
		},
		WSTG: map[string]standards.WSTGTest{
			"WSTG-INFO-01": {ID: "WSTG-INFO-01", Category: "Information Gathering", Name: "Conduct Network Recon", Reference: "https://owasp.org"},
			"WSTG-INFO-02": {ID: "WSTG-INFO-02", Category: "Information Gathering", Name: "Map Application Architecture", Reference: "https://owasp.org"},
			"WSTG-ATHN-01": {ID: "WSTG-ATHN-01", Category: "Authentication", Name: "Test Credentials Transported over an Encrypted Channel", Reference: "https://owasp.org"},
		},
	}
}

// coverageFixtureInput returns the shared fixture: ASVS level 1-2 + WSTG,
// with one result per verdict class plus an out-of-scope L3 pass.
func coverageFixtureInput() CoverageInput {
	return CoverageInput{
		Catalog: fixtureCatalog(),
		Scope: []CoverageScope{
			{Standard: db.StandardASVS, MaxLevel: 2},
			{Standard: db.StandardWSTG},
		},
		Results: []*db.ControlResult{
			{TaskID: 1, Standard: db.StandardASVS, RequirementID: "V1.1.1", Verdict: db.VerdictPass, Method: "automated", EvidenceRef: "snap-1"},
			{TaskID: 1, Standard: db.StandardASVS, RequirementID: "V1.1.2", Verdict: db.VerdictFail, Method: "manual", LinkedFindings: []int64{12, 15}},
			{TaskID: 1, Standard: db.StandardASVS, RequirementID: "V1.2.9", Verdict: db.VerdictNotApplicable, Reason: "no uploads"},
			{TaskID: 1, Standard: db.StandardASVS, RequirementID: "V1.2.10", Verdict: db.VerdictInconclusive, Method: "agent"},
			{TaskID: 1, Standard: db.StandardWSTG, RequirementID: "WSTG-INFO-01", Verdict: db.VerdictPartial, Method: "automated", EvidenceRef: "snap-2"},
			{TaskID: 1, Standard: db.StandardWSTG, RequirementID: "WSTG-INFO-02", Verdict: db.VerdictNotTested},
			// Out of scope: ASVS level 3 with MaxLevel 2.
			{TaskID: 1, Standard: db.StandardASVS, RequirementID: "V2.1.1", Verdict: db.VerdictPass, Method: "manual", EvidenceRef: "snap-3"},
		},
	}
}

func TestBuildCoverageCounts(t *testing.T) {
	summaries, rows, outOfScope := BuildCoverage(coverageFixtureInput())
	if len(summaries) != 2 {
		t.Fatalf("want 2 summaries, got %d", len(summaries))
	}
	asvs, wstg := summaries[0], summaries[1]
	if asvs.Standard != db.StandardASVS || asvs.MaxLevel != 2 {
		t.Fatalf("first summary should be ASVS L2: %+v", asvs)
	}
	if asvs.InScope != 5 || asvs.NotApplicable != 1 || asvs.Applicable != 4 || asvs.Verified != 2 {
		t.Errorf("ASVS counts: in=%d n/a=%d applicable=%d verified=%d, want 5/1/4/2",
			asvs.InScope, asvs.NotApplicable, asvs.Applicable, asvs.Verified)
	}
	if asvs.Pass != 1 || asvs.Fail != 1 || asvs.Inconclusive != 1 || asvs.NoResult != 1 {
		t.Errorf("ASVS verdicts: pass=%d fail=%d inconclusive=%d noresult=%d, want 1/1/1/1",
			asvs.Pass, asvs.Fail, asvs.Inconclusive, asvs.NoResult)
	}
	if asvs.Partial != 0 || asvs.NotTested != 0 {
		t.Errorf("ASVS unexpected partial/not_tested: %d/%d", asvs.Partial, asvs.NotTested)
	}
	if wstg.Standard != db.StandardWSTG || wstg.InScope != 3 || wstg.Applicable != 3 || wstg.Verified != 1 {
		t.Errorf("WSTG counts: %+v, want in=3 applicable=3 verified=1", wstg)
	}
	if wstg.Partial != 1 || wstg.NotTested != 1 || wstg.NoResult != 1 {
		t.Errorf("WSTG verdicts: partial=%d not_tested=%d noresult=%d, want 1/1/1",
			wstg.Partial, wstg.NotTested, wstg.NoResult)
	}
	if outOfScope != 1 {
		t.Errorf("outOfScope = %d, want 1", outOfScope)
	}
	wantOrder := []string{"V1.1.1", "V1.1.2", "V1.2.9", "V1.2.10", "V10.1.1"}
	var got []string
	for _, r := range rows {
		if r.Standard == db.StandardASVS {
			got = append(got, r.RequirementID)
		}
	}
	if strings.Join(got, ",") != strings.Join(wantOrder, ",") {
		t.Errorf("ASVS row order = %v, want %v", got, wantOrder)
	}
	// The out-of-scope L3 pass must not appear as a row.
	for _, r := range rows {
		if r.RequirementID == "V2.1.1" {
			t.Error("out-of-scope V2.1.1 rendered as a row")
		}
	}
}

func TestNaturalLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"V1.2.9", "V1.2.10", true},
		{"V1.2.10", "V1.2.9", false},
		{"V9.1.1", "V10.1.1", true},
		{"V10.1.1", "V9.1.1", false},
		{"WSTG-INFO-1", "WSTG-INFO-2", true},
		{"WSTG-INFO-2", "WSTG-INFO-10", true},
		{"WSTG-ATHN-01", "WSTG-INFO-01", true},
		{"V1.1.1", "V1.1.1", false},
		{"V1.1.1", "V1.1.2", true},
		{"V1.1", "V1.1.1", true},
		{"v2", "V10", false}, // lowercase 'v' > uppercase 'V' byte-wise
		{"", "a", true},
		{"a", "", false},
		{"", "", false},
		{"V007", "V7", false}, // equal numeric value; longer remainder loses
		{"V7", "V007", true},
	}
	for _, tc := range cases {
		if got := naturalLess(tc.a, tc.b); got != tc.want {
			t.Errorf("naturalLess(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCoverageMarkdownGolden(t *testing.T) {
	in := coverageFixtureInput()
	generated := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	got := CoverageMarkdown(in, generated, locale.En)
	goldenPath := filepath.Join("testdata", "coverage_en.golden.md")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(want) {
		t.Errorf("coverage markdown differs from golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestCoverageMarkdownNoComplianceClaim(t *testing.T) {
	in := coverageFixtureInput()
	generated := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	en := CoverageMarkdown(in, generated, locale.En)
	if n := strings.Count(strings.ToLower(en), "complian"); n != 1 {
		t.Errorf("En output has %d occurrences of %q, want exactly 1 (the disclaimer)", n, "complian")
	}
	ko := CoverageMarkdown(in, generated, locale.Ko)
	if !strings.Contains(ko, "이 보고서는 검증한 내용과 방법을 명시합니다. 인증이나 컴플라이언스 선언이 아닙니다.") {
		t.Error("Ko output missing the Korean disclaimer")
	}
	if strings.Contains(strings.ToLower(ko), "complian") {
		t.Error("Ko output contains an English compliance claim")
	}
}
