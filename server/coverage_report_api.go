package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/advisory"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/locale"
	"github.com/Autumn-27/artex/report"
	"github.com/Autumn-27/artex/standards"
)

// coverageReport renders a task's standards coverage report as Markdown, JSON or
// CSV. The catalog is the embedded standards set; the verdicts are the task's
// control_results rows. The scope is ASVS up to ?asvs_level (default 2) plus
// all WSTG tests. With no asvs_level param, a blueprint-provisioned instance
// takes the scope from its own test profiles (E.4); an explicit asvs_level
// always wins.
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
	wstgInScope := true
	rawLevel := r.URL.Query().Get("asvs_level")
	if rawLevel == "" {
		// No explicit level: a blueprint-provisioned instance reports the
		// scope of its own test profiles (E.4).
		if names, ok := blueprintStandards(s); ok {
			asvsLevel = standards.ProfilesMaxASVSLevel(names)
			wstgInScope = standards.ProfilesIncludeWSTG(names)
		}
	} else {
		level, err := strconv.Atoi(rawLevel)
		if err != nil || level < 1 || level > 3 {
			writeErr(w, 400, locale.Text(responseLanguage(w), "bad asvs_level: ")+rawLevel)
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
	scope := []report.CoverageScope{
		{Standard: db.StandardASVS, MaxLevel: asvsLevel},
	}
	if wstgInScope {
		scope = append(scope, report.CoverageScope{Standard: db.StandardWSTG})
	}
	in := report.CoverageInput{
		Catalog: catalog,
		Scope:   scope,
		Results: results,
	}

	// Markdown gains the "Findings by OWASP Top 10" section (E.5c): the task's
	// findings with their refs, plus the embedded crosswalk for the advisory ASVS
	// suggestions. JSON/CSV stay unchanged. A nil findings list would drop the
	// section, so an empty non-nil slice keeps it rendered.
	//
	// Markdown also gains the "Advisories" section (E.7f): the task's assets,
	// and the KEV catalog and NSM register embedded in the binary. Instances
	// never fetch — the embedded snapshot is the freshest thing they have.
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
		var assets []*db.Asset
		if as := s.assetStore(); as != nil {
			assets, err = as.ListAssetsForExport(db.AssetExportFilter{TaskID: taskID}, nil)
			if err != nil {
				writeError(w, 500, err)
				return
			}
		}
		kevVersion, kev, err := advisory.EmbeddedKEV()
		if err != nil {
			writeError(w, 500, err)
			return
		}
		nsm, err := advisory.EmbeddedNSMRegister()
		if err != nil {
			writeError(w, 500, err)
			return
		}
		in.Assets = assets
		in.KEVVersion = kevVersion
		in.KEV = kev
		in.NSM = nsm
		in.Now = time.Now()
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

// blueprintStandards reads the test-profile names recorded by blueprint
// provisioning (E.4). ok is false when the instance was not provisioned from
// a blueprint with standards, so the caller keeps its default scope.
func blueprintStandards(s *Server) (names []string, ok bool) {
	raw, found, err := s.m.pg.GetSetting("blueprint.standards")
	if err != nil || !found {
		return nil, false
	}
	for _, n := range strings.Split(raw, ",") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	return names, len(names) > 0
}
