package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// TestReportFindingClassificationRefs (E.2b-3): refs supplied at record time are
// checked against the embedded standards catalog and stored with the finding.
// A bad ref is reported back in refs_rejected and never fails or undoes the
// finding. DB-backed: skips without ARTEX_TEST_DSN.
func TestReportFindingClassificationRefs(t *testing.T) {
	d := testDB(t)
	defer d.Close()
	task, err := d.CreateTask("refs report", "fixture", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(task.ID)
	ts := NewToolSet(nil, "")
	ts.ts, ts.taskID = d.Exploration(task.ExplorationID), task.ID
	call := func(body string) string {
		t.Helper()
		res, err := ts.addFinding().Call(t.Context(), json.RawMessage(body), nil)
		if err != nil {
			t.Fatal(err)
		}
		return res.Flatten()
	}

	// Finding with two good refs and one bad asvs ref.
	got := call(`{"vulnclass":"REFS","severity":"low","summary":"refs fixture","refs":[{"scheme":"cwe","ref":"CWE-79"},{"scheme":"owasp-top10","ref":"A05:2025"},{"scheme":"asvs","ref":"V99.99.99"}]}`)
	lines := strings.SplitN(got, "\n", 2)
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "finding recorded: ") {
		t.Fatalf("finding not recorded: %s", got)
	}
	var out struct {
		FindingID int64 `json:"finding_id,string"`
		NodeID    int64 `json:"finding_node_id,string"`
		RefsAdded []struct {
			Scheme string `json:"scheme"`
			Ref    string `json:"ref"`
		} `json:"refs_added"`
		RefsRejected []struct {
			Scheme string `json:"scheme"`
			Ref    string `json:"ref"`
			Error  string `json:"error"`
		} `json:"refs_rejected"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &out); err != nil {
		t.Fatal(err)
	}
	if out.FindingID <= 0 {
		t.Fatalf("no finding id: %s", got)
	}
	if len(out.RefsAdded) != 2 {
		t.Fatalf("want 2 refs_added, got %d: %s", len(out.RefsAdded), got)
	}
	if out.RefsAdded[0].Scheme != "cwe" || out.RefsAdded[0].Ref != "CWE-79" ||
		out.RefsAdded[1].Scheme != "owasp-top10" || out.RefsAdded[1].Ref != "A05:2025" {
		t.Fatalf("wrong refs_added: %+v", out.RefsAdded)
	}
	if len(out.RefsRejected) != 1 || out.RefsRejected[0].Scheme != "asvs" || out.RefsRejected[0].Ref != "V99.99.99" || out.RefsRejected[0].Error == "" {
		t.Fatalf("bad asvs ref not rejected with an error: %+v", out.RefsRejected)
	}
	// The finding itself survived the bad ref.
	if f, err := d.GetFinding(out.FindingID); err != nil || f == nil {
		t.Fatalf("finding missing after bad ref: %v", err)
	}
	// The two good refs are stored.
	stored, err := d.ListFindingRefs(out.FindingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored[0].Scheme != "cwe" || stored[0].Ref != "CWE-79" ||
		stored[1].Scheme != "owasp-top10" || stored[1].Ref != "A05:2025" {
		t.Fatalf("stored refs mismatch: %+v", stored)
	}

	// A finding without refs behaves exactly as before: no refs_* keys in the result.
	got = call(`{"vulnclass":"REFS","severity":"low","summary":"no refs"}`)
	if strings.Contains(got, "refs_added") || strings.Contains(got, "refs_rejected") {
		t.Fatalf("refs keys present without refs: %s", got)
	}
	lines = strings.SplitN(got, "\n", 2)
	var plain db.RecordedFinding
	if err := json.Unmarshal([]byte(lines[1]), &plain); err != nil {
		t.Fatal(err)
	}
	if plain.FindingID <= 0 {
		t.Fatalf("plain finding missing id: %s", got)
	}
	stored, err = d.ListFindingRefs(plain.FindingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 0 {
		t.Fatalf("unexpected stored refs: %+v", stored)
	}
}

// TestReportFindingRefsSchemaAdvertised (DB-free): the tool's parameter schema
// advertises the optional "refs" array with the scheme enum.
func TestReportFindingRefsSchemaAdvertised(t *testing.T) {
	schema := NewToolSet(nil, "").addFinding().InputSchema()
	props, _ := schema["properties"].(map[string]any)
	refs, ok := props["refs"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no refs property: %+v", props)
	}
	if refs["type"] != "array" {
		t.Fatalf("refs type = %v, want array", refs["type"])
	}
	items, ok := refs["items"].(map[string]any)
	if !ok {
		t.Fatalf("refs items not an object schema: %+v", refs["items"])
	}
	itemProps, _ := items["properties"].(map[string]any)
	scheme, ok := itemProps["scheme"].(map[string]any)
	if !ok {
		t.Fatalf("refs item has no scheme property: %+v", itemProps)
	}
	enum, ok := scheme["enum"].([]string)
	if !ok || strings.Join(enum, ",") != "cwe,asvs,wstg,owasp-top10,cvss" {
		t.Fatalf("scheme enum mismatch: %#v", scheme["enum"])
	}
	if _, ok := itemProps["ref"].(map[string]any); !ok {
		t.Fatalf("refs item has no ref property: %+v", itemProps)
	}
	if req, _ := items["required"].([]string); len(req) != 2 || req[0] != "scheme" || req[1] != "ref" {
		t.Fatalf("refs item required mismatch: %#v", items["required"])
	}
	// refs itself stays optional.
	for _, r := range schema["required"].([]string) {
		if r == "refs" {
			t.Fatalf("refs must be optional: %#v", schema["required"])
		}
	}
}
