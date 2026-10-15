package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Autumn-27/artex/advisory"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/locale"
	"github.com/Autumn-27/artex/standards"
)

// Coverage report renders, in Markdown, what a task verified against the
// standards catalog. The wording never claims compliance: it states what was
// verified and how (see the fixed disclaimer in the Limitations section).

// CoverageScope selects one standard's requirements for the report. For ASVS,
// requirements with Level <= MaxLevel are in scope; MaxLevel is ignored for
// WSTG, where every catalog test is in scope.
type CoverageScope struct {
	Standard string
	MaxLevel int
}

// CoverageInput bundles the catalog, the selected scope and the task's control
// results. Results whose standard/requirement is not in scope are reported
// separately and never rendered as rows.
//
// Findings and Crosswalk are only consumed by the Markdown renderer's "Findings
// by OWASP Top 10" section: a nil Findings leaves that section out entirely (so
// the E.5a report is unchanged), while a non-nil but empty list renders the
// "no findings" sentence. Crosswalk supplies the advisory ASVS suggestions and
// may be nil.
//
// KEV and NSM are only consumed by the Markdown renderer's "Advisories"
// section (E.7f): a nil KEV and a nil NSM leave that section out entirely (so
// the earlier goldens stay byte-identical), while non-nil lists render it. Assets
// are the task's asset inventory the KEV records are matched against, and Now
// only selects the "recent" NSM window.
type CoverageInput struct {
	Catalog    *standards.Catalog
	Scope      []CoverageScope
	Results    []*db.ControlResult
	Findings   []*db.DBFinding
	Crosswalk  []standards.CrosswalkLink
	Assets     []*db.Asset
	KEVVersion string
	KEV        []advisory.Record
	NSM        []advisory.RegisterEntry
	Now        time.Time
}

// CoverageRow is one in-scope requirement with its recorded result. An empty
// Verdict means no result was recorded for the requirement.
type CoverageRow struct {
	Standard       string  `json:"standard"`
	RequirementID  string  `json:"requirement_id"`
	Level          int     `json:"level"`
	Verdict        string  `json:"verdict"`
	Method         string  `json:"method"`
	EvidenceRef    string  `json:"evidence_ref"`
	Reason         string  `json:"reason"`
	LinkedFindings []int64 `json:"linked_findings"`
}

// CoverageSummary counts one scope entry. InScope counts every rendered row;
// Applicable excludes not_applicable; Verified counts pass+fail+partial.
type CoverageSummary struct {
	Standard      string `json:"standard"`
	MaxLevel      int    `json:"max_level"`
	InScope       int    `json:"in_scope"`
	Applicable    int    `json:"applicable"`
	Verified      int    `json:"verified"`
	Pass          int    `json:"pass"`
	Fail          int    `json:"fail"`
	Partial       int    `json:"partial"`
	NotApplicable int    `json:"not_applicable"`
	Inconclusive  int    `json:"inconclusive"`
	NotTested     int    `json:"not_tested"`
	NoResult      int    `json:"no_result"`
}

// BuildCoverage pairs each in-scope catalog requirement with its control
// result. Rows are ordered by requirement id in natural order (V1.2.10 after
// V1.2.9). Results outside the selected scope are counted in outOfScope.
func BuildCoverage(in CoverageInput) (summaries []CoverageSummary, rows []CoverageRow, outOfScope int) {
	byReq := map[string]*db.ControlResult{}
	for _, r := range in.Results {
		if r == nil {
			continue
		}
		byReq[r.Standard+"\x00"+r.RequirementID] = r
	}
	seen := map[string]bool{}
	for _, sc := range in.Scope {
		sum := CoverageSummary{Standard: sc.Standard, MaxLevel: sc.MaxLevel}
		var scoped []CoverageRow
		switch sc.Standard {
		case db.StandardASVS:
			if in.Catalog != nil {
				for _, req := range in.Catalog.ASVS {
					if int(req.Level) > sc.MaxLevel {
						continue
					}
					scoped = append(scoped, CoverageRow{
						Standard: sc.Standard, RequirementID: req.ID, Level: int(req.Level),
					})
				}
			}
		case db.StandardWSTG:
			if in.Catalog != nil {
				for _, t := range in.Catalog.WSTG {
					scoped = append(scoped, CoverageRow{
						Standard: sc.Standard, RequirementID: t.ID,
					})
				}
			}
		}
		sort.Slice(scoped, func(i, j int) bool {
			return naturalLess(scoped[i].RequirementID, scoped[j].RequirementID)
		})
		for i := range scoped {
			row := &scoped[i]
			key := row.Standard + "\x00" + row.RequirementID
			if seen[key] {
				continue
			}
			seen[key] = true
			if r := byReq[key]; r != nil {
				row.Verdict = r.Verdict
				row.Method = r.Method
				row.EvidenceRef = r.EvidenceRef
				row.Reason = r.Reason
				row.LinkedFindings = r.LinkedFindings
			}
			countRow(&sum, row.Verdict)
			rows = append(rows, *row)
		}
		sum.InScope = len(scoped)
		sum.Applicable = sum.InScope - sum.NotApplicable
		sum.Verified = sum.Pass + sum.Fail + sum.Partial
		summaries = append(summaries, sum)
	}
	for _, r := range in.Results {
		if r == nil {
			continue
		}
		if !seen[r.Standard+"\x00"+r.RequirementID] {
			outOfScope++
		}
	}
	return summaries, rows, outOfScope
}

func countRow(sum *CoverageSummary, verdict string) {
	switch verdict {
	case db.VerdictPass:
		sum.Pass++
	case db.VerdictFail:
		sum.Fail++
	case db.VerdictPartial:
		sum.Partial++
	case db.VerdictNotApplicable:
		sum.NotApplicable++
	case db.VerdictInconclusive:
		sum.Inconclusive++
	case db.VerdictNotTested:
		sum.NotTested++
	default:
		sum.NoResult++
	}
}

// naturalLess compares requirement ids so digit runs order numerically:
// V1.2.10 sorts after V1.2.9, and WSTG-INFO-2 after WSTG-INFO-1.
func naturalLess(a, b string) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		da, db_ := isDigit(a[i]), isDigit(b[j])
		if da && db_ {
			si, sj := i, j
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na := strings.TrimLeft(a[si:i], "0")
			nb := strings.TrimLeft(b[sj:j], "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			// Equal numeric value: the run with fewer digits sorts first ("V7" < "V007").
			if (i - si) != (j - sj) {
				return (i - si) < (j - sj)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		if a[i] != b[j] {
			return a[i] < b[j]
		}
		i++
		j++
	}
	return len(a)-i < len(b)-j
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// standardName is the display name of a standard in the report.
func standardName(lang locale.Lang, standard string) string {
	switch standard {
	case db.StandardASVS:
		return "ASVS 5.0"
	case db.StandardWSTG:
		return "WSTG"
	}
	return standard
}

// coverageScopeLabel renders the scope text: "level 1" / "levels 1–2" for
// ASVS, "all tests" for WSTG.
func coverageScopeLabel(lang locale.Lang, sc CoverageScope) string {
	if sc.Standard == db.StandardWSTG {
		return locale.Text(lang, "all tests")
	}
	if sc.MaxLevel <= 1 {
		return locale.Text(lang, "level 1")
	}
	return fmt.Sprintf(locale.Text(lang, "levels 1–%d"), sc.MaxLevel)
}

// CoverageMarkdown renders the coverage report: a summary sentence and table
// per standard, the coverage matrix, and a limitations section. The words
// "compliant"/"compliance" appear only in the final disclaimer.
func CoverageMarkdown(in CoverageInput, generated time.Time, langs ...locale.Lang) string {
	lang := locale.First(langs)
	summaries, rows, outOfScope := BuildCoverage(in)

	var b strings.Builder
	b.WriteString(locale.Text(lang, "# Coverage report\n\n"))
	fmt.Fprintf(&b, locale.Text(lang, "Generated: %s\n\n"), generated.UTC().Format("2006-01-02 15:04 UTC"))

	b.WriteString(locale.Text(lang, "## Summary\n\n"))
	for _, s := range summaries {
		name := standardName(lang, s.Standard)
		scope := coverageScopeLabel(lang, CoverageScope{Standard: s.Standard, MaxLevel: s.MaxLevel})
		fmt.Fprintf(&b, locale.Text(lang, "Verified against %d of %d applicable %s requirements (scope: %s).\n\n"), s.Verified, s.Applicable, name, scope)
	}
	b.WriteString(locale.Text(lang, "| Standard | In scope | Not applicable | Verified | Pass | Fail | Partial | Inconclusive | Not tested | No result |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n"))
	for _, s := range summaries {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %d | %d | %d | %d |\n",
			standardName(lang, s.Standard), s.InScope, s.NotApplicable, s.Verified,
			s.Pass, s.Fail, s.Partial, s.Inconclusive, s.NotTested, s.NoResult)
	}
	b.WriteString("\n")

	b.WriteString(locale.Text(lang, "## Coverage matrix\n\n"))
	for _, s := range summaries {
		fmt.Fprintf(&b, "### %s\n\n", standardName(lang, s.Standard))
		b.WriteString(locale.Text(lang, "| Requirement | Level | Verdict | Method | Evidence | Findings |\n| --- | --- | --- | --- | --- | --- |\n"))
		for _, row := range rows {
			if row.Standard != s.Standard {
				continue
			}
			level := "—"
			if row.Standard == db.StandardASVS {
				level = fmt.Sprintf("%d", row.Level)
			}
			verdict := row.Verdict
			if verdict == "" {
				verdict = "—"
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
				escapeMDCell(row.RequirementID), level, escapeMDCell(verdict),
				escapeMDCell(row.Method), escapeMDCell(row.EvidenceRef), findingsCell(row.LinkedFindings))
		}
		b.WriteString("\n")
	}

	b.WriteString(locale.Text(lang, "## Limitations\n\n"))
	for _, s := range summaries {
		name := standardName(lang, s.Standard)
		fmt.Fprintf(&b, "- "+locale.Text(lang, "%d %s requirements were not tested, %d were inconclusive, and %d have no recorded result.\n"),
			s.NotTested, name, s.Inconclusive, s.NoResult)
	}
	if outOfScope > 0 {
		fmt.Fprintf(&b, "- "+locale.Text(lang, "%d recorded results were outside the selected scope and are not counted.\n"), outOfScope)
	}
	b.WriteString("- " + locale.Text(lang, "This report states what was verified and how. It is not a certification or a statement of compliance.") + "\n")
	if in.Findings != nil {
		b.WriteString("\n")
		b.WriteString(findingsByTop10Markdown(in, lang))
	}
	// The Advisories section (E.7f) renders only when at least one advisory
	// source is supplied, so reports without one stay byte-identical to the
	// earlier goldens.
	if in.KEV != nil || in.NSM != nil {
		b.WriteString("\n")
		b.WriteString(advisoriesMarkdown(in, lang))
	}
	return b.String()
}

// maxRecentNSM caps the "Recent NSM alerts" list; the newest entries win.
const maxRecentNSM = 10

// recentNSMDays is the lookback window for "recent" NSM alerts.
const recentNSMDays = 30

// advisoriesMarkdown renders the "Advisories" section (E.7f): which advisory
// sources were checked and how fresh they are, the KEV entries that may apply
// to the task's assets, and the recent NSM alerts. Every KEV row is a
// candidate for a human/agent check against the version actually in use,
// never a finding — the section says so in its own words.
func advisoriesMarkdown(in CoverageInput, lang locale.Lang) string {
	var b strings.Builder
	b.WriteString(locale.Text(lang, "## Advisories\n\n"))
	latestNSM := ""
	for _, e := range in.NSM {
		if e.Date > latestNSM {
			latestNSM = e.Date
		}
	}
	if latestNSM == "" {
		latestNSM = "—"
	}
	fmt.Fprintf(&b, locale.Text(lang, "Sources checked: CISA KEV catalog %s (%d entries); NSM vulnerability alerts register (%d alerts, latest %s).\n\n"),
		in.KEVVersion, len(in.KEV), len(in.NSM), latestNSM)

	b.WriteString(locale.Text(lang, "### Known exploited vulnerabilities that may apply\n\n"))
	matches := advisory.MatchAssets(in.KEV, in.Assets)
	if len(matches) == 0 {
		b.WriteString(locale.Text(lang, "No KEV entries matched the detected technologies of this task's assets.") + "\n")
	} else {
		b.WriteString(locale.Text(lang, "| Asset | Technology | CVE | Vulnerability | Added |\n| --- | --- | --- | --- | --- |\n"))
		for _, m := range matches {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
				escapeMDCell(assetLabel(m.AssetID, in.Assets)),
				escapeMDCell(m.Technology),
				escapeMDCell(m.Record.ID),
				escapeMDCell(m.Record.Title),
				m.Record.Date.Format("2006-01-02"))
		}
		b.WriteString("\n")
	}
	b.WriteString(locale.Text(lang, "These are candidates to verify against the actual version in use, not findings.") + "\n")

	b.WriteString("\n")
	b.WriteString(locale.Text(lang, "### Recent NSM alerts\n\n"))
	cutoff := in.Now.UTC().AddDate(0, 0, -recentNSMDays).Format("2006-01-02")
	recent := make([]advisory.RegisterEntry, 0, len(in.NSM))
	for _, e := range in.NSM {
		if e.Date > cutoff && e.Date <= in.Now.UTC().Format("2006-01-02") {
			recent = append(recent, e)
		}
	}
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].Date > recent[j].Date })
	if len(recent) > maxRecentNSM {
		recent = recent[:maxRecentNSM]
	}
	if len(recent) == 0 {
		b.WriteString(locale.Text(lang, "No NSM alerts in the last 30 days.") + "\n")
		return b.String()
	}
	for _, e := range recent {
		if len(e.CVEs) > 0 {
			fmt.Fprintf(&b, "- %s — [%s](%s) (%s)\n", e.Date, e.Title, e.Link, strings.Join(e.CVEs, ", "))
			continue
		}
		fmt.Fprintf(&b, "- %s — [%s](%s)\n", e.Date, e.Title, e.Link)
	}
	return b.String()
}

// assetLabel renders an asset reference for the KEV table: "#<id> <value>",
// where value is the asset's URL, domain or IP — the first non-empty one.
// An asset with none of the three renders as its bare "#<id>".
func assetLabel(id int64, assets []*db.Asset) string {
	for _, a := range assets {
		if a == nil || a.ID != id {
			continue
		}
		for _, v := range []string{a.URL, a.Domain, a.IP} {
			if v != "" {
				return fmt.Sprintf("#%d %s", id, v)
			}
		}
		return fmt.Sprintf("#%d", id)
	}
	return fmt.Sprintf("#%d", id)
}

// coverageJSONDoc is the machine-readable coverage report envelope.
type coverageJSONDoc struct {
	Generated  string            `json:"generated"`
	Summaries  []CoverageSummary `json:"summaries"`
	Rows       []CoverageRow     `json:"rows"`
	OutOfScope int               `json:"out_of_scope"`
}

// CoverageJSON renders the coverage matrix as indented JSON: the generation
// timestamp in RFC3339 UTC, the per-standard summaries, every in-scope row,
// and the count of results that fell outside the selected scope. Empty
// summaries/rows marshal as [] (never null) so consumers can iterate blindly;
// a row's linked_findings is normalized to [] the same way.
func CoverageJSON(in CoverageInput, generated time.Time) ([]byte, error) {
	summaries, rows, outOfScope := BuildCoverage(in)
	if summaries == nil {
		summaries = []CoverageSummary{}
	}
	if rows == nil {
		rows = []CoverageRow{}
	}
	for i := range rows {
		if rows[i].LinkedFindings == nil {
			rows[i].LinkedFindings = []int64{}
		}
	}
	doc := coverageJSONDoc{
		Generated:  generated.UTC().Format(time.RFC3339),
		Summaries:  summaries,
		Rows:       rows,
		OutOfScope: outOfScope,
	}
	return json.MarshalIndent(doc, "", "  ")
}

// CoverageCSV renders the coverage matrix as CSV with a UTF-8 BOM for Excel.
// One row per in-scope requirement, in the same order as the Markdown matrix.
// The Level column is empty for WSTG (its tests carry no ASVS level) and the
// Verdict column is empty when no result was recorded. Linked findings render
// as a comma-separated id list ("12,15").
func CoverageCSV(in CoverageInput, langs ...locale.Lang) []byte {
	lang := locale.First(langs)
	_, rows, _ := BuildCoverage(in)

	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF") // UTF-8 BOM
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{
		locale.Text(lang, "Standard"),
		locale.Text(lang, "Requirement"),
		locale.Text(lang, "Level"),
		locale.Text(lang, "Verdict"),
		locale.Text(lang, "Method"),
		locale.Text(lang, "Evidence"),
		locale.Text(lang, "Findings"),
		locale.Text(lang, "Reason"),
	})
	for _, row := range rows {
		level := ""
		if row.Standard == db.StandardASVS {
			level = fmt.Sprintf("%d", row.Level)
		}
		findings := make([]string, 0, len(row.LinkedFindings))
		for _, id := range row.LinkedFindings {
			findings = append(findings, fmt.Sprintf("%d", id))
		}
		_ = w.Write([]string{
			row.Standard,
			row.RequirementID,
			level,
			row.Verdict,
			row.Method,
			row.EvidenceRef,
			strings.Join(findings, ","),
			row.Reason,
		})
	}
	w.Flush()
	return buf.Bytes()
}

// findingsCell renders linked finding ids as "#12, #15" (empty when none).
func findingsCell(ids []int64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("#%d", id))
	}
	return strings.Join(parts, ", ")
}

// escapeMDCell escapes pipe characters so user-supplied text cannot break a
// Markdown table row.
func escapeMDCell(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// maxAdvisoryASVS caps the advisory ASVS suggestions listed per finding; the
// remainder is summarized as "(+N more)".
const maxAdvisoryASVS = 8

// findingsByTop10Markdown renders the "Findings by OWASP Top 10" section: the
// task's findings grouped by their owasp-top10 refs, each with its CWE and ASVS
// refs plus the ASVS requirements suggested by the crosswalk (advisory).
func findingsByTop10Markdown(in CoverageInput, lang locale.Lang) string {
	var b strings.Builder
	b.WriteString(locale.Text(lang, "## Findings by OWASP Top 10\n\n"))
	b.WriteString(locale.Text(lang, "ASVS (advisory) lists requirements linked to the finding's CWE through the OpenCRE crosswalk. These links are not authoritative.") + "\n")

	groups := map[string][]*db.DBFinding{}
	var unclassified []*db.DBFinding
	for _, f := range in.Findings {
		if f == nil || f.Status == db.FindingFalsePositive || f.Status == db.FindingDuplicate {
			continue
		}
		ids := findingRefValues(f, db.SchemeOWASPTop10)
		if len(ids) == 0 {
			unclassified = append(unclassified, f)
			continue
		}
		for _, id := range ids {
			groups[id] = append(groups[id], f)
		}
	}

	if len(groups) == 0 && len(unclassified) == 0 {
		b.WriteString(locale.Text(lang, "No findings recorded for this task.") + "\n")
		return b.String()
	}

	var order []string
	if in.Catalog != nil {
		for id := range in.Catalog.Top10 {
			if len(groups[id]) > 0 {
				order = append(order, id)
			}
		}
	}
	sort.Slice(order, func(i, j int) bool { return naturalLess(order[i], order[j]) })
	for _, id := range order {
		b.WriteString("\n")
		writeTop10Group(&b, in, lang, top10Heading(in, id), groups[id])
	}
	if len(unclassified) > 0 {
		b.WriteString("\n")
		writeTop10Group(&b, in, lang, locale.Text(lang, "Unclassified"), unclassified)
	}
	return b.String()
}

// top10Heading renders a category heading body: "A01:2025 — <Title>", or the bare
// id when the catalog has no such category.
func top10Heading(in CoverageInput, id string) string {
	if in.Catalog != nil {
		if c, ok := in.Catalog.Top10[id]; ok && c.Title != "" {
			return id + " — " + c.Title
		}
	}
	return id
}

// writeTop10Group writes one group's heading and findings table. Findings sort by
// severity (critical, high, medium, low, then others) and then by id.
func writeTop10Group(b *strings.Builder, in CoverageInput, lang locale.Lang, heading string, findings []*db.DBFinding) {
	fmt.Fprintf(b, "### %s\n\n", escapeMDCell(heading))
	b.WriteString(locale.Text(lang, "| Finding | Severity | Status | CWE | ASVS | ASVS (advisory) |\n| --- | --- | --- | --- | --- | --- |\n"))
	sorted := make([]*db.DBFinding, len(findings))
	copy(sorted, findings)
	sort.SliceStable(sorted, func(i, j int) bool {
		si, sj := severityRank(sorted[i].Severity), severityRank(sorted[j].Severity)
		if si != sj {
			return si > sj
		}
		return sorted[i].ID < sorted[j].ID
	})
	for _, f := range sorted {
		name := f.Name
		if name == "" {
			name = f.VulnClass
		}
		fmt.Fprintf(b, "| #%d %s | %s | %s | %s | %s | %s |\n",
			f.ID,
			escapeMDCell(name),
			escapeMDCell(f.Severity),
			escapeMDCell(f.Status),
			escapeMDCell(strings.Join(findingRefValues(f, db.SchemeCWE), ", ")),
			escapeMDCell(strings.Join(findingRefValues(f, db.SchemeASVS), ", ")),
			escapeMDCell(advisoryASVSCell(in, f, lang)))
	}
}

// severityRank orders severities for the findings table; unknown values sort last.
func severityRank(severity string) int {
	switch severity {
	case db.SeverityCritical:
		return 4
	case db.SeverityHigh:
		return 3
	case db.SeverityMedium:
		return 2
	case db.SeverityLow:
		return 1
	}
	return 0
}

// findingRefValues returns the finding's refs of one scheme in natural order
// ("CWE-9" before "CWE-79", "V1.1.10" after "V1.1.9").
func findingRefValues(f *db.DBFinding, scheme string) []string {
	var out []string
	for _, r := range f.Refs {
		if r != nil && r.Scheme == scheme {
			out = append(out, r.Ref)
		}
	}
	sort.Slice(out, func(i, j int) bool { return naturalLess(out[i], out[j]) })
	return out
}

// advisoryASVSCell lists the ASVS ids the crosswalk links to the finding's CWE
// refs, minus the ASVS refs already listed, in natural order, capped at
// maxAdvisoryASVS with a "(+N more)" tail.
func advisoryASVSCell(in CoverageInput, f *db.DBFinding, lang locale.Lang) string {
	cwes := map[string]bool{}
	for _, cwe := range findingRefValues(f, db.SchemeCWE) {
		cwes[cwe] = true
	}
	listed := map[string]bool{}
	for _, asvs := range findingRefValues(f, db.SchemeASVS) {
		listed[asvs] = true
	}
	seen := map[string]bool{}
	var suggested []string
	for _, l := range in.Crosswalk {
		if l.From.Scheme != db.SchemeASVS || l.To.Scheme != db.SchemeCWE || !cwes[l.To.ID] {
			continue
		}
		id := l.From.ID
		if listed[id] || seen[id] {
			continue
		}
		seen[id] = true
		suggested = append(suggested, id)
	}
	sort.Slice(suggested, func(i, j int) bool { return naturalLess(suggested[i], suggested[j]) })
	if len(suggested) <= maxAdvisoryASVS {
		return strings.Join(suggested, ", ")
	}
	more := len(suggested) - maxAdvisoryASVS
	return strings.Join(suggested[:maxAdvisoryASVS], ", ") + fmt.Sprintf(locale.Text(lang, " (+%d more)"), more)
}
