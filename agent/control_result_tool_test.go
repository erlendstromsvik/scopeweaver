package agent

import (
	"encoding/json"
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
