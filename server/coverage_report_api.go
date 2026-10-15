package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/locale"
	"github.com/Autumn-27/artex/report"
	"github.com/Autumn-27/artex/standards"
)

// coverageReport renders a task's standards coverage report as Markdown, JSON or
// CSV. The catalog is the embedded standards set; the verdicts are the task's
// control_results rows. The scope is ASVS up to ?asvs_level (default 2) plus all
// WSTG tests; E.4 will take the scope from the blueprint instead.
//
//	format = md (default) | json | csv
//	asvs_level = 1..3 (default 2)
func (s *Server) coverageReport(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "md"
	}
	if format != "md" && format != "json" && format != "csv" {
		writeErr(w, 400, locale.Text(responseLanguage(w), "bad format: ")+format)
		return
	}
	asvsLevel := 2
	if raw := r.URL.Query().Get("asvs_level"); raw != "" {
		level, err := strconv.Atoi(raw)
		if err != nil || level < 1 || level > 3 {
			writeErr(w, 400, locale.Text(responseLanguage(w), "bad asvs_level: ")+raw)
			return
		}
		asvsLevel = level
	}
	catalog, err := standards.Embedded()
	if err != nil {
		writeError(w, 500, err)
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	results, err := s.m.pg.ListControlResults(taskID)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	in := report.CoverageInput{
		Catalog: catalog,
		Scope: []report.CoverageScope{
			{Standard: db.StandardASVS, MaxLevel: asvsLevel},
			{Standard: db.StandardWSTG},
		},
		Results: results,
	}

	// Markdown gains the "Findings by OWASP Top 10" section (E.5c): the task's
	// findings with their refs, plus the embedded crosswalk for the advisory ASVS
	// suggestions. JSON/CSV stay unchanged. A nil findings list would drop the
	// section, so an empty non-nil slice keeps it rendered.
	if format == "md" {
		findings, err := s.m.pg.ListFindingsForExport(db.FindingFilter{TaskID: t.ID}, nil)
		if err != nil {
			writeError(w, 500, err)
			return
		}
		if findings == nil {
			findings = []*db.DBFinding{}
		}
		crosswalk, err := standards.EmbeddedCrosswalk()
		if err != nil {
			writeError(w, 500, err)
			return
		}
		in.Findings = findings
		in.Crosswalk = crosswalk
	}

	now := time.Now()
	stamp := now.Format("20060102-150405")
	filename := fmt.Sprintf(`attachment; filename="coverage-%s-%s.`, t.ID, stamp)
	setDownload := func(contentType, ext string) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", filename+ext+`"`)
	}

	switch format {
	case "json":
		body, err := report.CoverageJSON(in, now)
		if err != nil {
			writeError(w, 500, err)
			return
		}
		setDownload("application/json; charset=utf-8", "json")
		_, _ = w.Write(body)
	case "csv":
		setDownload("text/csv; charset=utf-8", "csv")
		_, _ = w.Write(report.CoverageCSV(in, locale.FromRequest(r)))
	default: // md
		setDownload("text/markdown; charset=utf-8", "md")
		_, _ = w.Write([]byte(report.CoverageMarkdown(in, now, locale.FromRequest(r))))
	}
}
