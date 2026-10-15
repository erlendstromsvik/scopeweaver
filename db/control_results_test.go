package db

import (
	"slices"
	"testing"
)

// newControlTask opens the test DB and creates a throwaway task, registering its
// cleanup. It skips when postgres is unavailable.
func newControlTask(t *testing.T) (*DB, int64) {
	t.Helper()
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	t.Cleanup(func() { d.Close() })
	tk, err := d.CreateTask("control results test", "Goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.DeleteTask(tk.ID) })
	return d, tk.ID
}

// TestUpsertControlResultHappyPaths covers one valid verdict of each shape and
// checks the row round-trips through Get, JSONB arrays included.
func TestUpsertControlResultHappyPaths(t *testing.T) {
	d, taskID := newControlTask(t)

	// fail needs a real finding to cite.
	fid, err := d.AddFinding(taskID, 0, "XSS", "Reflected XSS", "high", "s", "poc", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		in   ControlResultInput
	}{
		{"pass with evidence+assets", ControlResultInput{
			TaskID: taskID, Standard: StandardASVS, RequirementID: "V1.1.1",
			Verdict: VerdictPass, Method: MethodAutomated,
			EvidenceRef: "snap-123", AssetsCovered: []int64{1, 2}, Actor: "tester",
		}},
		{"fail with linked finding", ControlResultInput{
			TaskID: taskID, Standard: StandardASVS, RequirementID: "V2.1.1",
			Verdict: VerdictFail, Method: MethodManual, LinkedFindings: []int64{fid},
		}},
		{"not_applicable with reason", ControlResultInput{
			TaskID: taskID, Standard: StandardWSTG, RequirementID: "WSTG-INFO-01",
			Verdict: VerdictNotApplicable, Reason: "no auth surface in scope",
		}},
		{"not_tested bare", ControlResultInput{
			TaskID: taskID, Standard: StandardWSTG, RequirementID: "WSTG-INFO-02",
			Verdict: VerdictNotTested,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := d.UpsertControlResult(tc.in)
			if err != nil {
				t.Fatalf("UpsertControlResult: %v", err)
			}
			if id <= 0 {
				t.Fatalf("want positive id, got %d", id)
			}
			got, err := d.GetControlResult(tc.in.TaskID, tc.in.Standard, tc.in.RequirementID)
			if err != nil {
				t.Fatalf("GetControlResult: %v", err)
			}
			if got == nil {
				t.Fatal("GetControlResult returned nil for a row just written")
			}
			if got.Verdict != tc.in.Verdict || got.Method != tc.in.Method {
				t.Fatalf("verdict/method mismatch: got (%q,%q) want (%q,%q)",
					got.Verdict, got.Method, tc.in.Verdict, tc.in.Method)
			}
			if got.EvidenceRef != tc.in.EvidenceRef || got.Reason != tc.in.Reason {
				t.Fatalf("evidence/reason mismatch: got (%q,%q) want (%q,%q)",
					got.EvidenceRef, got.Reason, tc.in.EvidenceRef, tc.in.Reason)
			}
			if !slices.Equal(got.AssetsCovered, tc.in.AssetsCovered) {
				t.Fatalf("assets round-trip: got %v want %v", got.AssetsCovered, tc.in.AssetsCovered)
			}
			if !slices.Equal(got.LinkedFindings, tc.in.LinkedFindings) {
				t.Fatalf("findings round-trip: got %v want %v", got.LinkedFindings, tc.in.LinkedFindings)
			}
		})
	}
}

// TestUpsertControlResultIntegrity verifies each integrity rule rejects a bad
// write (and nothing is persisted for it).
func TestUpsertControlResultIntegrity(t *testing.T) {
	d, taskID := newControlTask(t)

	cases := []struct {
		name string
		in   ControlResultInput
	}{
		{"fail without finding", ControlResultInput{
			TaskID: taskID, Standard: StandardASVS, RequirementID: "V9.1.1", Verdict: VerdictFail,
		}},
		{"pass without evidence", ControlResultInput{
			TaskID: taskID, Standard: StandardASVS, RequirementID: "V9.1.2",
			Verdict: VerdictPass, AssetsCovered: []int64{1},
		}},
		{"pass without assets", ControlResultInput{
			TaskID: taskID, Standard: StandardASVS, RequirementID: "V9.1.3",
			Verdict: VerdictPass, EvidenceRef: "snap-1",
		}},
		{"not_applicable without reason", ControlResultInput{
			TaskID: taskID, Standard: StandardASVS, RequirementID: "V9.1.4", Verdict: VerdictNotApplicable,
		}},
		{"bad verdict", ControlResultInput{
			TaskID: taskID, Standard: StandardASVS, RequirementID: "V9.1.5", Verdict: "maybe",
		}},
		{"bad method", ControlResultInput{
			TaskID: taskID, Standard: StandardASVS, RequirementID: "V9.1.6",
			Verdict: VerdictNotTested, Method: "guessing",
		}},
		{"unknown standard", ControlResultInput{
			TaskID: taskID, Standard: "pci", RequirementID: "X", Verdict: VerdictNotTested,
		}},
		{"empty requirement id", ControlResultInput{
			TaskID: taskID, Standard: StandardASVS, RequirementID: "", Verdict: VerdictNotTested,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := d.UpsertControlResult(tc.in); err == nil {
				t.Fatalf("expected rejection for %s, got nil error", tc.name)
			}
			// a rejected write must not persist (skip the empty-id case).
			if tc.in.RequirementID != "" {
				got, err := d.GetControlResult(tc.in.TaskID, tc.in.Standard, tc.in.RequirementID)
				if err != nil {
					t.Fatalf("GetControlResult: %v", err)
				}
				if got != nil {
					t.Fatalf("rejected write should not persist, found %+v", got)
				}
			}
		})
	}
}

// TestUpsertControlResultReplacesRow verifies a second write to the same key
// updates in place: the row count stays 1, the verdict changes, and updated_at
// advances past created_at.
func TestUpsertControlResultReplacesRow(t *testing.T) {
	d, taskID := newControlTask(t)

	fid, err := d.AddFinding(taskID, 0, "SQLI", "SQL injection", "high", "s", "poc", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}

	first := ControlResultInput{
		TaskID: taskID, Standard: StandardASVS, RequirementID: "V5.3.4",
		Verdict: VerdictNotTested,
	}
	id1, err := d.UpsertControlResult(first)
	if err != nil {
		t.Fatal(err)
	}

	// flip to fail with a linked finding under the same key.
	second := ControlResultInput{
		TaskID: taskID, Standard: StandardASVS, RequirementID: "V5.3.4",
		Verdict: VerdictFail, Method: MethodAgent, LinkedFindings: []int64{fid},
	}
	id2, err := d.UpsertControlResult(second)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("upsert should reuse the same row: id1=%d id2=%d", id1, id2)
	}

	var count int
	if err := d.QueryRow(
		`SELECT count(*) FROM control_results WHERE task_id=$1 AND standard=$2 AND requirement_id=$3`,
		taskID, StandardASVS, "V5.3.4").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("want exactly 1 row after upsert, got %d", count)
	}

	got, err := d.GetControlResult(taskID, StandardASVS, "V5.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictFail || !slices.Equal(got.LinkedFindings, []int64{fid}) {
		t.Fatalf("row not updated: %+v", got)
	}
	if !got.UpdatedAt.After(got.CreatedAt) {
		t.Fatalf("updated_at (%v) should advance past created_at (%v)", got.UpdatedAt, got.CreatedAt)
	}
}

// TestListControlResults checks List returns all of a task's rows ordered by
// (standard, requirement_id), and GetControlResult returns nil for an absent key.
func TestListControlResults(t *testing.T) {
	d, taskID := newControlTask(t)

	seed := []ControlResultInput{
		{TaskID: taskID, Standard: StandardWSTG, RequirementID: "WSTG-INFO-02", Verdict: VerdictNotTested},
		{TaskID: taskID, Standard: StandardASVS, RequirementID: "V2.1.1", Verdict: VerdictNotTested},
		{TaskID: taskID, Standard: StandardASVS, RequirementID: "V1.1.1",
			Verdict: VerdictPass, EvidenceRef: "snap-1", AssetsCovered: []int64{7}},
	}
	for _, in := range seed {
		if _, err := d.UpsertControlResult(in); err != nil {
			t.Fatalf("seed %s/%s: %v", in.Standard, in.RequirementID, err)
		}
	}

	list, err := d.ListControlResults(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("want 3 results, got %d", len(list))
	}
	wantOrder := []struct{ std, req string }{
		{StandardASVS, "V1.1.1"},
		{StandardASVS, "V2.1.1"},
		{StandardWSTG, "WSTG-INFO-02"},
	}
	for i, w := range wantOrder {
		if list[i].Standard != w.std || list[i].RequirementID != w.req {
			t.Fatalf("order[%d]: got %s/%s want %s/%s",
				i, list[i].Standard, list[i].RequirementID, w.std, w.req)
		}
	}
	// the pass row's JSONB asset array must survive the list query too.
	if !slices.Equal(list[0].AssetsCovered, []int64{7}) {
		t.Fatalf("list JSONB round-trip: got %v want [7]", list[0].AssetsCovered)
	}

	absent, err := d.GetControlResult(taskID, StandardASVS, "V0.0.0")
	if err != nil {
		t.Fatalf("GetControlResult(absent): %v", err)
	}
	if absent != nil {
		t.Fatalf("want nil for absent requirement, got %+v", absent)
	}
}
