package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
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

// TestCoverageJSON parses the JSON export back and checks the counts, the row
// order, and that a row without findings serializes linked_findings as [] rather
// than null.
func TestCoverageJSON(t *testing.T) {
	in := coverageFixtureInput()
	generated := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	body, err := CoverageJSON(in, generated)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Generated  string            `json:"generated"`
		Summaries  []CoverageSummary `json:"summaries"`
		Rows       []CoverageRow     `json:"rows"`
		OutOfScope int               `json:"out_of_scope"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("parse: %v (%s)", err, body)
	}
	if doc.Generated != "2026-10-15T12:00:00Z" {
		t.Errorf("generated = %q, want RFC3339 UTC", doc.Generated)
	}
	if len(doc.Summaries) != 2 {
		t.Fatalf("summaries = %d, want 2", len(doc.Summaries))
	}
	if doc.Summaries[0].Standard != db.StandardASVS || doc.Summaries[0].MaxLevel != 2 || doc.Summaries[0].InScope != 5 || doc.Summaries[0].Verified != 2 {
		t.Errorf("asvs summary = %+v", doc.Summaries[0])
	}
	if doc.Summaries[1].Standard != db.StandardWSTG || doc.Summaries[1].InScope != 3 {
		t.Errorf("wstg summary = %+v", doc.Summaries[1])
	}
	if doc.OutOfScope != 1 {
		t.Errorf("out_of_scope = %d, want 1", doc.OutOfScope)
	}
	wantOrder := []string{"V1.1.1", "V1.1.2", "V1.2.9", "V1.2.10", "V10.1.1", "WSTG-ATHN-01", "WSTG-INFO-01", "WSTG-INFO-02"}
	var got []string
	for _, r := range doc.Rows {
		got = append(got, r.RequirementID)
	}
	if strings.Join(got, ",") != strings.Join(wantOrder, ",") {
		t.Errorf("row order = %v, want %v", got, wantOrder)
	}
	// The V1.1.2 fail row carries findings 12,15; a row without findings must
	// serialize as [] not null.
	byID := map[string]CoverageRow{}
	for _, r := range doc.Rows {
		byID[r.RequirementID] = r
	}
	if lf := byID["V1.1.2"].LinkedFindings; len(lf) != 2 || lf[0] != 12 || lf[1] != 15 {
		t.Errorf("V1.1.2 linked_findings = %v, want [12 15]", lf)
	}
	if byID["V1.1.1"].LinkedFindings == nil {
		t.Error("V1.1.1 linked_findings is nil; want empty slice")
	}
	// Raw JSON must show [] for a no-findings row, never null.
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	rows := raw["rows"].([]any)
	for _, e := range rows {
		m := e.(map[string]any)
		if m["requirement_id"] == "V1.1.1" {
			if lf, ok := m["linked_findings"].([]any); !ok || len(lf) != 0 {
				t.Fatalf("V1.1.1 linked_findings not []: %#v", m["linked_findings"])
			}
		}
	}
}

// TestCoverageCSV checks the BOM, the localized header, and two known rows: the
// ASVS pass row (level + evidence, empty findings) and the WSTG partial row
// (empty level).
func TestCoverageCSV(t *testing.T) {
	in := coverageFixtureInput()
	body := CoverageCSV(in, locale.En)
	if !bytes.HasPrefix(body, []byte("\xEF\xBB\xBF")) {
		t.Fatal("missing UTF-8 BOM")
	}
	r := csv.NewReader(bytes.NewReader(body[3:]))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	wantHeader := []string{"Standard", "Requirement", "Level", "Verdict", "Method", "Evidence", "Findings", "Reason"}
	if len(records) == 0 || strings.Join(records[0], "|") != strings.Join(wantHeader, "|") {
		t.Fatalf("header = %v, want %v", records[0], wantHeader)
	}
	// 8 in-scope rows (5 asvs + 3 wstg) plus the header.
	if len(records) != 9 {
		t.Fatalf("rows = %d, want 9 (header + 8)", len(records))
	}
	byReq := map[string][]string{}
	for _, rec := range records[1:] {
		byReq[rec[1]] = rec
	}
	// ASVS pass row: level 1, verdict pass, method automated, evidence snap-1,
	// no findings, no reason.
	pass := byReq["V1.1.1"]
	if pass == nil {
		t.Fatal("missing V1.1.1 row")
	}
	if pass[0] != "asvs" || pass[2] != "1" || pass[3] != "pass" || pass[4] != "automated" || pass[5] != "snap-1" || pass[6] != "" || pass[7] != "" {
		t.Errorf("V1.1.1 row = %v", pass)
	}
	// WSTG partial row: empty level, verdict partial, findings empty.
	wstg := byReq["WSTG-INFO-01"]
	if wstg == nil {
		t.Fatal("missing WSTG-INFO-01 row")
	}
	if wstg[0] != "wstg" || wstg[2] != "" || wstg[3] != "partial" || wstg[5] != "snap-2" {
		t.Errorf("WSTG-INFO-01 row = %v", wstg)
	}
	// The fail row carries findings "12,15".
	fail := byReq["V1.1.2"]
	if fail == nil || fail[6] != "12,15" {
		t.Errorf("V1.1.2 findings cell = %q, want 12,15", fail[6])
	}
	// Korean header is localized.
	ko := CoverageCSV(in, locale.Ko)
	koRecs, err := csv.NewReader(bytes.NewReader(ko[3:])).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if koRecs[0][0] != "표준" || koRecs[0][3] != "판정" {
		t.Errorf("ko header = %v", koRecs[0])
	}
}

// findingsCatalog adds the Top 10 categories the findings fixture groups into.
func findingsCatalog() *standards.Catalog {
	c := fixtureCatalog()
	c.Top10 = map[string]standards.Top10Category{
		"A01:2025": {ID: "A01:2025", Title: "Broken Access Control"},
		"A05:2025": {ID: "A05:2025", Title: "Injection"},
	}
	return c
}

func ref(scheme, id string) *db.FindingRef { return &db.FindingRef{Scheme: scheme, Ref: id} }

func link(fromScheme, fromID, toScheme, toID string) standards.CrosswalkLink {
	return standards.CrosswalkLink{
		From:       standards.Ref{Scheme: fromScheme, ID: fromID},
		To:         standards.Ref{Scheme: toScheme, ID: toID},
		Source:     "test",
		Confidence: "low",
	}
}

// findingsFixtureInput is the E.5c fixture: four findings, one of which is a
// duplicate that must be excluded, plus a small crosswalk.
func findingsFixtureInput() CoverageInput {
	in := coverageFixtureInput()
	in.Catalog = findingsCatalog()
	in.Crosswalk = []standards.CrosswalkLink{
		link(db.SchemeASVS, "V5.1.1", db.SchemeCWE, "CWE-79"),
		link(db.SchemeASVS, "V5.2.2", db.SchemeCWE, "CWE-79"),
		link(db.SchemeASVS, "V1.1.1", db.SchemeCWE, "CWE-89"),
	}
	in.Findings = []*db.DBFinding{
		{ID: 1, Name: "XSS in search", Severity: "high", Status: "confirmed",
			Refs: []*db.FindingRef{ref(db.SchemeCWE, "CWE-79"), ref(db.SchemeOWASPTop10, "A05:2025"), ref(db.SchemeASVS, "V5.1.1")}},
		{ID: 2, Name: "IDOR", Severity: "critical", Status: "confirmed",
			Refs: []*db.FindingRef{ref(db.SchemeOWASPTop10, "A01:2025")}},
		{ID: 3, Name: "Verbose error", Severity: "low", Status: "pending"},
		{ID: 4, Name: "Dup XSS", Severity: "high", Status: db.FindingDuplicate,
			Refs: []*db.FindingRef{ref(db.SchemeCWE, "CWE-79"), ref(db.SchemeOWASPTop10, "A05:2025")}},
	}
	return in
}

// TestFindingsByTop10Golden renders the E.5c section once and checks the golden:
// A01 holds #2, A05 holds #1 (ASVS V5.1.1, advisory V5.2.2 only), Unclassified
// holds #3, and the duplicate #4 is absent.
func TestFindingsByTop10Golden(t *testing.T) {
	in := findingsFixtureInput()
	generated := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	got := CoverageMarkdown(in, generated, locale.En)
	goldenPath := filepath.Join("testdata", "coverage_findings_en.golden.md")
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
		t.Errorf("findings markdown differs from golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFindingsByTop10Groups checks the grouping, ordering and advisory cell
// directly, without the golden file.
func TestFindingsByTop10Groups(t *testing.T) {
	in := findingsFixtureInput()
	got := CoverageMarkdown(in, time.Unix(0, 0).UTC(), locale.En)

	// Headings in catalog order, only non-empty groups, Unclassified last.
	a01 := strings.Index(got, "### A01:2025 — Broken Access Control")
	a05 := strings.Index(got, "### A05:2025 — Injection")
	unc := strings.Index(got, "### Unclassified")
	if a01 < 0 || a05 < 0 || unc < 0 {
		t.Fatalf("missing a group heading: a01=%d a05=%d unc=%d\n%s", a01, a05, unc, got)
	}
	if !(a01 < a05 && a05 < unc) {
		t.Errorf("group order wrong: a01=%d a05=%d unc=%d", a01, a05, unc)
	}
	// #2 in A01, #1 in A05, #3 in Unclassified, #4 nowhere.
	a01Body := got[a01:a05]
	a05Body := got[a05:unc]
	uncBody := got[unc:]
	if !strings.Contains(a01Body, "#2 IDOR") {
		t.Errorf("A01 missing #2 IDOR:\n%s", a01Body)
	}
	if strings.Contains(a01Body, "#1 ") {
		t.Errorf("A01 should not contain #1:\n%s", a01Body)
	}
	if !strings.Contains(a05Body, "#1 XSS in search") {
		t.Errorf("A05 missing #1:\n%s", a05Body)
	}
	// #1's ASVS column lists V5.1.1; advisory lists V5.2.2 only (V5.1.1 already listed).
	for _, line := range strings.Split(a05Body, "\n") {
		if strings.Contains(line, "#1 XSS in search") {
			cols := strings.Split(line, "|")
			if len(cols) != 8 {
				t.Fatalf("unexpected column count %d in %q", len(cols)-1, line)
			}
			if strings.TrimSpace(cols[5]) != "V5.1.1" {
				t.Errorf("ASVS column = %q, want V5.1.1", strings.TrimSpace(cols[5]))
			}
			if strings.TrimSpace(cols[6]) != "V5.2.2" {
				t.Errorf("advisory column = %q, want V5.2.2", strings.TrimSpace(cols[6]))
			}
		}
	}
	if !strings.Contains(uncBody, "#3 Verbose error") {
		t.Errorf("Unclassified missing #3:\n%s", uncBody)
	}
	if strings.Contains(got, "#4") || strings.Contains(got, "Dup XSS") {
		t.Errorf("duplicate finding #4 must be excluded:\n%s", got)
	}
}

// TestAdvisoryASVSCap checks a CWE that maps to 10 advisory ASVS ids shows 8 and
// "(+2 more)", in natural order.
func TestAdvisoryASVSCap(t *testing.T) {
	in := findingsFixtureInput()
	// Ten distinct asvs -> CWE-79 links, none already listed on the finding.
	in.Crosswalk = nil
	for _, id := range []string{"V1.1.1", "V1.1.2", "V1.1.3", "V1.1.4", "V1.1.5", "V1.1.6", "V1.1.7", "V1.1.8", "V1.1.9", "V1.1.10"} {
		in.Crosswalk = append(in.Crosswalk, link(db.SchemeASVS, id, db.SchemeCWE, "CWE-79"))
	}
	f := &db.DBFinding{ID: 9, Name: "Many", Severity: "high", Status: "confirmed",
		Refs: []*db.FindingRef{ref(db.SchemeCWE, "CWE-79"), ref(db.SchemeOWASPTop10, "A05:2025")}}
	cell := advisoryASVSCell(in, f, locale.En)
	want := "V1.1.1, V1.1.2, V1.1.3, V1.1.4, V1.1.5, V1.1.6, V1.1.7, V1.1.8 (+2 more)"
	if cell != want {
		t.Errorf("advisory cell = %q, want %q", cell, want)
	}
}

// TestFindingsEmptySection checks an empty (non-nil) findings list renders the
// "no findings" sentence and no group headings.
func TestFindingsEmptySection(t *testing.T) {
	in := findingsFixtureInput()
	in.Findings = []*db.DBFinding{}
	got := CoverageMarkdown(in, time.Unix(0, 0).UTC(), locale.En)
	if !strings.Contains(got, "No findings recorded for this task.") {
		t.Errorf("missing no-findings sentence:\n%s", got)
	}
	if strings.Contains(got, "### A01") || strings.Contains(got, "### Unclassified") {
		t.Errorf("empty findings should render no groups:\n%s", got)
	}
}
