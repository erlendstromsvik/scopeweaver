package db

import (
	"encoding/json"
	"time"

	"github.com/Autumn-27/artex/locale"
)

// ControlResult is a row in control_results: the current verdict for one
// security-standard requirement under one task. One row per (task_id, standard,
// requirement_id); UpsertControlResult replaces it in place.
type ControlResult struct {
	ID             int64
	TaskID         int64
	Standard       string  // scheme key: 'asvs' | 'wstg'
	RequirementID  string  // e.g. 'V1.1.1', 'WSTG-INFO-01'
	Verdict        string  // one of the six control verdicts
	Method         string  // '' or one of the control methods
	EvidenceRef    string  // traffic_evidence_snapshots.id or a pointer
	Reason         string  // required for not_applicable; free note otherwise
	Actor          string  // who recorded the verdict
	AssetsCovered  []int64 // asset ids proving what was checked
	LinkedFindings []int64 // findings.id list (required for a fail)
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ControlResultInput is the payload for UpsertControlResult.
type ControlResultInput struct {
	TaskID         int64
	Standard       string
	RequirementID  string
	Verdict        string
	Method         string
	EvidenceRef    string
	Reason         string
	Actor          string
	AssetsCovered  []int64
	LinkedFindings []int64
}

// Control verdicts (control_results.verdict). Kept in sync with the schema CHECK.
const (
	VerdictPass          = "pass"           // Checked and compliant.
	VerdictFail          = "fail"           // Checked and non-compliant; cites a finding.
	VerdictPartial       = "partial"        // Partially satisfied.
	VerdictNotApplicable = "not_applicable" // Out of scope; cites a reason.
	VerdictNotTested     = "not_tested"     // Never checked (distinct from pass).
	VerdictInconclusive  = "inconclusive"   // Checked without a decisive result.
)

// ValidControlVerdict reports whether v is a known control verdict.
func ValidControlVerdict(v string) bool {
	switch v {
	case VerdictPass, VerdictFail, VerdictPartial, VerdictNotApplicable,
		VerdictNotTested, VerdictInconclusive:
		return true
	}
	return false
}

// Control methods (control_results.method). "" means unspecified.
const (
	MethodAutomated = "automated" // Automated scanner.
	MethodAgent     = "agent"     // Agent-driven check.
	MethodManual    = "manual"    // Manual testing.
	MethodDocument  = "document"  // Documentary evidence/review.
)

// ValidControlMethod reports whether m is a known method ("" allowed).
func ValidControlMethod(m string) bool {
	switch m {
	case "", MethodAutomated, MethodAgent, MethodManual, MethodDocument:
		return true
	}
	return false
}

// Known standard scheme keys (control_results.standard).
const (
	StandardASVS = "asvs" // OWASP ASVS.
	StandardWSTG = "wstg" // OWASP WSTG.
)

// ValidStandard reports whether s is a known scheme key.
func ValidStandard(s string) bool {
	switch s {
	case StandardASVS, StandardWSTG:
		return true
	}
	return false
}

// validateControlResult enforces the integrity rules that keep a verdict
// meaningful before it is written. These run server-side so a bad verdict never
// reaches the table regardless of caller.
//
// TODO(E.3b): resolve requirement_id against the standards catalog at the server
// layer (the db package stays free of the filesystem catalog).
func validateControlResult(in ControlResultInput) error {
	if in.TaskID <= 0 {
		return locale.Errorf("control result requires a task id")
	}
	if !ValidStandard(in.Standard) {
		return locale.Errorf("unknown standard %q (want asvs or wstg)", in.Standard)
	}
	if in.RequirementID == "" {
		return locale.Errorf("control result requires a requirement id")
	}
	if !ValidControlVerdict(in.Verdict) {
		return locale.Errorf("unknown control verdict %q", in.Verdict)
	}
	if !ValidControlMethod(in.Method) {
		return locale.Errorf("unknown control method %q", in.Method)
	}
	switch in.Verdict {
	case VerdictFail:
		// A failure must cite the finding(s) that prove it.
		if len(in.LinkedFindings) == 0 {
			return locale.Errorf("a fail verdict must link at least one finding")
		}
	case VerdictPass:
		// A pass must prove what was checked and where.
		if in.EvidenceRef == "" {
			return locale.Errorf("a pass verdict requires an evidence reference")
		}
		if len(in.AssetsCovered) == 0 {
			return locale.Errorf("a pass verdict requires at least one covered asset")
		}
	case VerdictNotApplicable:
		if in.Reason == "" {
			return locale.Errorf("a not_applicable verdict requires a reason")
		}
	}
	return nil
}

// controlResultSelectCols is the column list every control_results query selects,
// so scanControlResults stays in sync across callers.
const controlResultSelectCols = `id, task_id, standard, requirement_id, verdict, method,
	assets_covered, evidence_ref, linked_findings, reason, actor, created_at, updated_at`

// scanControlResults materializes rows selected via controlResultSelectCols.
func scanControlResults(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]*ControlResult, error) {
	var out []*ControlResult
	for rows.Next() {
		c := &ControlResult{}
		var assetsJSON, findingsJSON string
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Standard, &c.RequirementID, &c.Verdict, &c.Method,
			&assetsJSON, &c.EvidenceRef, &findingsJSON, &c.Reason, &c.Actor, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(assetsJSON), &c.AssetsCovered)
		_ = json.Unmarshal([]byte(findingsJSON), &c.LinkedFindings)
		out = append(out, c)
	}
	return out, rows.Err()
}

// jsonIDArray marshals an id list to a JSONB-ready string, normalizing nil to [].
func jsonIDArray(ids []int64) string {
	b, _ := json.Marshal(ids)
	if ids == nil {
		return "[]"
	}
	return string(b)
}

// UpsertControlResult validates the integrity rules then writes the current
// verdict for (task_id, standard, requirement_id), replacing any existing row for
// that key and bumping updated_at. Returns the row id.
func (d *DB) UpsertControlResult(in ControlResultInput) (int64, error) {
	if err := validateControlResult(in); err != nil {
		return 0, err
	}
	var id int64
	err := d.QueryRow(`
		INSERT INTO control_results
			(task_id, standard, requirement_id, verdict, method,
			 assets_covered, evidence_ref, linked_findings, reason, actor)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (task_id, standard, requirement_id) DO UPDATE SET
			verdict         = EXCLUDED.verdict,
			method          = EXCLUDED.method,
			assets_covered  = EXCLUDED.assets_covered,
			evidence_ref    = EXCLUDED.evidence_ref,
			linked_findings = EXCLUDED.linked_findings,
			reason          = EXCLUDED.reason,
			actor           = EXCLUDED.actor,
			updated_at      = now()
		RETURNING id`,
		in.TaskID, in.Standard, in.RequirementID, in.Verdict, in.Method,
		jsonIDArray(in.AssetsCovered), in.EvidenceRef, jsonIDArray(in.LinkedFindings),
		in.Reason, in.Actor,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ListControlResults returns every control result for a task, ordered by standard
// then requirement id.
func (d *DB) ListControlResults(taskID int64) ([]*ControlResult, error) {
	rows, err := d.Query(`
		SELECT `+controlResultSelectCols+`
		FROM control_results
		WHERE task_id = $1
		ORDER BY standard, requirement_id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanControlResults(rows)
}

// GetControlResult returns the control result for one requirement, or (nil, nil)
// when none has been recorded.
func (d *DB) GetControlResult(taskID int64, standard, requirementID string) (*ControlResult, error) {
	rows, err := d.Query(`
		SELECT `+controlResultSelectCols+`
		FROM control_results
		WHERE task_id = $1 AND standard = $2 AND requirement_id = $3`,
		taskID, standard, requirementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results, err := scanControlResults(rows)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}
