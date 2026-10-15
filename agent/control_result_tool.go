package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/locale"
	"github.com/Autumn-27/artex/standards"
	actool "github.com/Autumn-27/norma/tool"
)

// ControlResultWriter persists one control verdict. *db.ExplorationStore will
// satisfy it in E.3b-3b; tests inject a fake.
type ControlResultWriter interface {
	UpsertControlResult(db.ControlResultInput) (int64, error)
}

// controlResultWriter returns the writer record_control_result uses, or nil.
func (t *ToolSet) controlResultWriter() ControlResultWriter { return t.controlWriter }

func (t *ToolSet) recordControlResult() actool.CoreTool {
	return writeTool("record_control_result", locale.Text(t.language, "Record your verdict for one security-standard requirement (OWASP ASVS or WSTG) in this task. Use only for requirements you actually checked: pass needs evidence_ref and asset_ids; fail needs finding_ids of findings already reported with report_finding; not_applicable needs reason. Calling again for the same requirement replaces the earlier verdict."),
		obj(map[string]any{
			"standard":       str("asvs|wstg"),
			"requirement_id": str(locale.Text(t.language, "Requirement ID from the standard, e.g. V1.1.1 (ASVS) or WSTG-INFO-01 (WSTG)")),
			"verdict":        str("pass|fail|partial|not_applicable|not_tested|inconclusive"),
			"method":         str("agent|automated|manual|document"),
			"evidence_ref":   str(locale.Text(t.language, "Evidence reference proving the check, such as a traffic snapshot ID or a pointer to command output; required for pass")),
			"reason":         str(locale.Text(t.language, "Reason for the verdict; required for not_applicable")),
			"asset_ids":      map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": locale.Text(t.language, "IDs of the assets that were checked; required for pass")},
			"finding_ids":    map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": locale.Text(t.language, "finding_id values from report_finding that prove the failure; required for fail")},
		}, "standard", "requirement_id", "verdict"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			lang := locale.FromContext(ctx)
			if w := t.controlResultWriter(); w == nil || t.taskID <= 0 {
				return actool.Errorf(locale.Text(lang, "record_control_result requires task context; it is only available to agents running inside a task")), nil
			}
			var a struct {
				Standard      string            `json:"standard"`
				RequirementID string            `json:"requirement_id"`
				Verdict       string            `json:"verdict"`
				Method        string            `json:"method"`
				EvidenceRef   string            `json:"evidence_ref"`
				Reason        string            `json:"reason"`
				AssetIDs      []json.RawMessage `json:"asset_ids"`
				FindingIDs    []json.RawMessage `json:"finding_ids"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf(locale.ErrorMessage(lang, err)), nil
			}
			std := strings.ToLower(strings.TrimSpace(a.Standard))
			req := strings.ToUpper(strings.TrimSpace(a.RequirementID))
			verdict := strings.ToLower(strings.TrimSpace(a.Verdict))
			method := strings.ToLower(strings.TrimSpace(a.Method))
			if method == "" {
				method = db.MethodAgent
			}
			if !db.ValidStandard(std) {
				return actool.Errorf(locale.ErrorMessage(lang, locale.Errorf("unknown standard %q (want asvs or wstg)", std))), nil
			}
			cat, err := standards.Embedded()
			if err != nil {
				return actool.Errorf(locale.ErrorMessage(lang, err)), nil
			}
			if !cat.Resolve(std, req) {
				return actool.Errorf(fmt.Sprintf(locale.Text(lang, "unknown requirement %q in standard %q; check the ID against the standard"), req, std)), nil
			}
			w := t.controlResultWriter()
			id, err := w.UpsertControlResult(db.ControlResultInput{
				TaskID:         t.taskID,
				Standard:       std,
				RequirementID:  req,
				Verdict:        verdict,
				Method:         method,
				EvidenceRef:    strings.TrimSpace(a.EvidenceRef),
				Reason:         strings.TrimSpace(a.Reason),
				Actor:          t.worker,
				AssetsCovered:  pidList(a.AssetIDs),
				LinkedFindings: pidList(a.FindingIDs),
			})
			if err != nil {
				return actool.Errorf(locale.ErrorMessage(lang, err)), nil
			}
			return jsonResult(map[string]any{"control_result_id": id, "standard": std, "requirement_id": req, "verdict": verdict})
		})
}
