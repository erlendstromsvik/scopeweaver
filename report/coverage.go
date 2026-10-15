package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

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
type CoverageInput struct {
	Catalog *standards.Catalog
	Scope   []CoverageScope
	Results []*db.ControlResult
}

// CoverageRow is one in-scope requirement with its recorded result. An empty
// Verdict means no result was recorded for the requirement.
type CoverageRow struct {
	Standard       string
	RequirementID  string
	Level          int
	Verdict        string
	Method         string
	EvidenceRef    string
	Reason         string
	LinkedFindings []int64
}

// CoverageSummary counts one scope entry. InScope counts every rendered row;
// Applicable excludes not_applicable; Verified counts pass+fail+partial.
type CoverageSummary struct {
	Standard      string
	MaxLevel      int
	InScope       int
	Applicable    int
	Verified      int
	Pass          int
	Fail          int
	Partial       int
	NotApplicable int
	Inconclusive  int
	NotTested     int
	NoResult      int
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
	return b.String()
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
