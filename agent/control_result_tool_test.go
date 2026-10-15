package agent

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/locale"
)

type fakeControlWriter struct {
	got []db.ControlResultInput
	id  int64
	err error
}

func (f *fakeControlWriter) UpsertControlResult(in db.ControlResultInput) (int64, error) {
	f.got = append(f.got, in)
	return f.id, f.err
}

func newCRToolSet(w *fakeControlWriter) *ToolSet {
	ts := NewToolSet(nil, "worker-1")
	ts.SetTaskID(7)
	ts.controlWriter = w
	return ts
}

// TestRecordControlResultRejectsNotTested: agents may not record the system-only
// not_tested verdict (E.4 seeds it). The tool must refuse and never reach the writer.
func TestRecordControlResultRejectsNotTested(t *testing.T) {
	fake := &fakeControlWriter{}
	ts := newCRToolSet(fake)
	res, err := ts.recordControlResult().Call(t.Context(), json.RawMessage(`{"standard":"asvs","requirement_id":"V1.1.1","verdict":"not_tested"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Flatten(), "not_tested cannot be recorded by an agent") {
		t.Fatalf("want not_tested refusal, got IsError=%v %q", res.IsError, res.Flatten())
	}
	if len(fake.got) != 0 {
		t.Fatalf("writer called for not_tested: %+v", fake.got)
	}
}

// TestRecordControlResultNoWriterNoStore: with controlWriter nil and t.ts nil the
// tool must answer with the task-context refusal, not a nil-interface panic.
func TestRecordControlResultNoWriterNoStore(t *testing.T) {
	ts := NewToolSet(nil, "worker-1")
	ts.SetTaskID(7)
	res, err := ts.recordControlResult().Call(t.Context(), json.RawMessage(`{"standard":"asvs","requirement_id":"V1.1.1","verdict":"pass","evidence_ref":"snap-1","asset_ids":[1]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Flatten(), "task context") {
		t.Fatalf("want task-context refusal, got IsError=%v %q", res.IsError, res.Flatten())
	}
}

// TestRecordControlResultInWorkerTools: the tool is bound to the worker agent and
// not to mainagent (owner decision: worker only).
func TestRecordControlResultInWorkerTools(t *testing.T) {
	ts := NewToolSet(nil, "")
	inWorker := false
	for _, tool := range ts.WorkerTools() {
		if tool.Name() == "record_control_result" {
			inWorker = true
		}
	}
	if !inWorker {
		t.Fatal("record_control_result missing from WorkerTools")
	}
	for _, tool := range ts.MainAgentTools() {
		if tool.Name() == "record_control_result" {
			t.Fatal("record_control_result must not be in MainAgentTools")
		}
	}
}

// TestRecordControlResultSeedsWorkerOnly: the catalog seed binds the tool to the
// worker agent and to no other agent (mainagent/goals/planner/auto/pentest).
func TestRecordControlResultSeedsWorkerOnly(t *testing.T) {
	var seed *ToolSeed
	seeds := BuiltinToolSeeds()
	for i := range seeds {
		if seeds[i].Key == "record_control_result" {
			seed = &seeds[i]
		}
	}
	if seed == nil {
		t.Fatal("record_control_result not seeded")
	}
	if len(seed.Agents) != 1 || seed.Agents[0] != "worker" {
		t.Fatalf("record_control_result agents = %v, want [worker]", seed.Agents)
	}
}

// TestRecordControlResultPersists: a worker ToolSet whose ts is a real
// task-bound ExplorationStore writes through the passthrough; the row is visible
// via ListControlResults.
func TestRecordControlResultPersists(t *testing.T) {
	d := testDB(t)
	defer d.Close()
	tk, err := d.CreateTask("control result wiring test", "Goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(tk.ID)
	assetID, err := d.Assets().UpsertHTTPService(db.UpsertHTTPServiceReq{
		URL:    "http://cr-wire-test.example.org/",
		TaskID: tk.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := NewToolSet(d.Exploration(tk.ExplorationID), "worker-1")
	ts.SetTaskID(tk.ID)
	res, err := ts.recordControlResult().Call(t.Context(), json.RawMessage(
		`{"standard":"asvs","requirement_id":"V1.1.1","verdict":"pass","evidence_ref":"snap-9","asset_ids":[`+strconv.FormatInt(assetID, 10)+`]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %q", res.Flatten())
	}
	rows, err := d.ListControlResults(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 control result, got %d", len(rows))
	}
	got := rows[0]
	if got.Standard != "asvs" || got.RequirementID != "V1.1.1" || got.Verdict != "pass" || got.Method != "agent" || got.EvidenceRef != "snap-9" || got.Actor != "worker-1" {
		t.Fatalf("persisted row wrong: %+v", got)
	}
	if len(got.AssetsCovered) != 1 || got.AssetsCovered[0] != assetID {
		t.Fatalf("AssetsCovered = %v, want [%d]", got.AssetsCovered, assetID)
	}
}

func TestRecordControlResultRequiresTask(t *testing.T) {
	ts := NewToolSet(nil, "")
	res, err := ts.recordControlResult().Call(t.Context(), json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Flatten(), "task context") {
		t.Fatalf("want task-context refusal, got IsError=%v %q", res.IsError, res.Flatten())
	}
}

func TestRecordControlResultUnknownRequirement(t *testing.T) {
	fake := &fakeControlWriter{}
	ts := newCRToolSet(fake)
	res, err := ts.recordControlResult().Call(t.Context(), json.RawMessage(`{"standard":"asvs","requirement_id":"V99.99.99","verdict":"not_tested"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Flatten(), "V99.99.99") {
		t.Fatalf("want unknown-requirement error, got IsError=%v %q", res.IsError, res.Flatten())
	}
	if len(fake.got) != 0 {
		t.Fatalf("writer called for unknown requirement: %+v", fake.got)
	}
}

func TestRecordControlResultUnknownStandard(t *testing.T) {
	fake := &fakeControlWriter{}
	ts := newCRToolSet(fake)
	res, err := ts.recordControlResult().Call(t.Context(), json.RawMessage(`{"standard":"top10","requirement_id":"A01:2025","verdict":"not_tested"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Flatten(), "asvs or wstg") {
		t.Fatalf("want unknown-standard error, got IsError=%v %q", res.IsError, res.Flatten())
	}
	if len(fake.got) != 0 {
		t.Fatalf("writer called for unknown standard: %+v", fake.got)
	}
}

func TestRecordControlResultPassNormalizes(t *testing.T) {
	fake := &fakeControlWriter{id: 42}
	ts := newCRToolSet(fake)
	res, err := ts.recordControlResult().Call(t.Context(), json.RawMessage(`{"standard":" ASVS ","requirement_id":"v1.1.1","verdict":"PASS","evidence_ref":"snap-1","asset_ids":[3,"4"]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %q", res.Flatten())
	}
	if len(fake.got) != 1 {
		t.Fatalf("want one write, got %d", len(fake.got))
	}
	got := fake.got[0]
	if got.TaskID != 7 || got.Standard != "asvs" || got.RequirementID != "V1.1.1" || got.Verdict != "pass" || got.Method != "agent" || got.EvidenceRef != "snap-1" || got.Actor != "worker-1" {
		t.Fatalf("normalization wrong: %+v", got)
	}
	if len(got.AssetsCovered) != 2 || got.AssetsCovered[0] != 3 || got.AssetsCovered[1] != 4 {
		t.Fatalf("AssetsCovered = %v, want [3 4]", got.AssetsCovered)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Flatten()), &out); err != nil {
		t.Fatal(err)
	}
	if id, _ := out["control_result_id"].(float64); id != 42 {
		t.Fatalf("control_result_id = %v, want 42", out["control_result_id"])
	}
}

func TestRecordControlResultWSTGFail(t *testing.T) {
	fake := &fakeControlWriter{id: 7}
	ts := newCRToolSet(fake)
	res, err := ts.recordControlResult().Call(t.Context(), json.RawMessage(`{"standard":"wstg","requirement_id":"WSTG-INFO-01","verdict":"fail","method":"manual","finding_ids":[9]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %q", res.Flatten())
	}
	if len(fake.got) != 1 {
		t.Fatalf("want one write, got %d", len(fake.got))
	}
	got := fake.got[0]
	if got.Method != "manual" {
		t.Fatalf("Method = %q, want manual", got.Method)
	}
	if len(got.LinkedFindings) != 1 || got.LinkedFindings[0] != 9 {
		t.Fatalf("LinkedFindings = %v, want [9]", got.LinkedFindings)
	}
}

func TestRecordControlResultWriterError(t *testing.T) {
	fake := &fakeControlWriter{err: locale.Errorf("a fail verdict must link at least one finding")}
	ts := newCRToolSet(fake)
	res, err := ts.recordControlResult().Call(t.Context(), json.RawMessage(`{"standard":"asvs","requirement_id":"V1.1.1","verdict":"fail"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Flatten(), "must link at least one finding") {
		t.Fatalf("want writer error surfaced, got IsError=%v %q", res.IsError, res.Flatten())
	}
}
