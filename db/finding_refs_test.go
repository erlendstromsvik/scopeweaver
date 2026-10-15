package db

import (
	"testing"
)

// newRefFinding opens the test DB, creates a throwaway task and one finding on it,
// and registers cleanup. It skips when postgres is unavailable.
func newRefFinding(t *testing.T) (*DB, int64) {
	t.Helper()
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	t.Cleanup(func() { d.Close() })
	tk, err := d.CreateTask("finding refs test", "Goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.DeleteTask(tk.ID) })
	fid, err := d.AddFinding(tk.ID, 0, "XSS", "Reflected XSS", "high", "s", "poc", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	return d, fid
}

// TestAddFindingRefEachScheme adds one valid ref of each scheme and checks List
// returns them all, ordered by scheme then ref.
func TestAddFindingRefEachScheme(t *testing.T) {
	d, fid := newRefFinding(t)

	refs := []struct{ scheme, ref string }{
		{SchemeCWE, "CWE-79"},
		{SchemeASVS, "V1.1.1"},
		{SchemeWSTG, "WSTG-INFO-01"},
		{SchemeOWASPTop10, "A05:2025"},
		{SchemeCVSS, "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N"},
	}
	for _, r := range refs {
		id, err := d.AddFindingRef(fid, r.scheme, r.ref)
		if err != nil {
			t.Fatalf("AddFindingRef(%s,%s): %v", r.scheme, r.ref, err)
		}
		if id <= 0 {
			t.Fatalf("AddFindingRef(%s,%s): want positive id, got %d", r.scheme, r.ref, id)
		}
	}

	got, err := d.ListFindingRefs(fid)
	if err != nil {
		t.Fatalf("ListFindingRefs: %v", err)
	}
	if len(got) != len(refs) {
		t.Fatalf("ListFindingRefs returned %d refs, want %d", len(got), len(refs))
	}
	// Ordered by scheme then ref.
	wantOrder := []string{"asvs", "cvss", "cwe", "owasp-top10", "wstg"}
	for i, want := range wantOrder {
		if got[i].Scheme != want {
			t.Fatalf("ref %d scheme = %q, want %q", i, got[i].Scheme, want)
		}
		if got[i].FindingID != fid {
			t.Fatalf("ref %d finding_id = %d, want %d", i, got[i].FindingID, fid)
		}
	}
}

// TestAddFindingRefTrimsRef trims surrounding whitespace before storing.
func TestAddFindingRefTrimsRef(t *testing.T) {
	d, fid := newRefFinding(t)

	id, err := d.AddFindingRef(fid, SchemeASVS, "  V2.1.1  ")
	if err != nil {
		t.Fatalf("AddFindingRef: %v", err)
	}
	got, err := d.ListFindingRefs(fid)
	if err != nil {
		t.Fatalf("ListFindingRefs: %v", err)
	}
	if len(got) != 1 || got[0].Ref != "V2.1.1" {
		t.Fatalf("stored ref = %+v, want trimmed V2.1.1", got)
	}
	if got[0].ID != id {
		t.Fatalf("listed id %d != returned id %d", got[0].ID, id)
	}
}

// TestAddFindingRefIdempotent adds the same (scheme,ref) twice and checks it
// collapses to one row with a stable id.
func TestAddFindingRefIdempotent(t *testing.T) {
	d, fid := newRefFinding(t)

	id1, err := d.AddFindingRef(fid, SchemeCWE, "CWE-89")
	if err != nil {
		t.Fatalf("first AddFindingRef: %v", err)
	}
	id2, err := d.AddFindingRef(fid, SchemeCWE, "CWE-89")
	if err != nil {
		t.Fatalf("second AddFindingRef: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("idempotent add returned different ids: %d then %d", id1, id2)
	}
	got, err := d.ListFindingRefs(fid)
	if err != nil {
		t.Fatalf("ListFindingRefs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want one row after duplicate add, got %d", len(got))
	}
}

// TestAddFindingRefRejectsInvalid covers the validation failures.
func TestAddFindingRefRejectsInvalid(t *testing.T) {
	d, fid := newRefFinding(t)

	cases := []struct {
		name   string
		scheme string
		ref    string
	}{
		{"unknown scheme", "stride", "S1"},
		{"empty ref", SchemeASVS, ""},
		{"whitespace-only ref", SchemeASVS, "   "},
		{"malformed cwe", SchemeCWE, "79"},
		{"malformed owasp-top10", SchemeOWASPTop10, "A05:2021"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := d.AddFindingRef(fid, tc.scheme, tc.ref); err == nil {
				t.Fatalf("AddFindingRef(%q,%q) = nil error, want rejection", tc.scheme, tc.ref)
			}
		})
	}
	// A zero finding id is rejected too.
	if _, err := d.AddFindingRef(0, SchemeASVS, "V1.1.1"); err == nil {
		t.Fatal("AddFindingRef with finding id 0 = nil error, want rejection")
	}
	// None of the rejected adds wrote a row.
	got, err := d.ListFindingRefs(fid)
	if err != nil {
		t.Fatalf("ListFindingRefs: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("rejected adds wrote %d rows, want 0", len(got))
	}
}

// TestSetFindingRefsReplaces checks SetFindingRefs replaces the whole set and
// collapses duplicate inputs.
func TestSetFindingRefsReplaces(t *testing.T) {
	d, fid := newRefFinding(t)

	if _, err := d.AddFindingRef(fid, SchemeCWE, "CWE-79"); err != nil {
		t.Fatalf("seed AddFindingRef: %v", err)
	}

	// Replace with a new set that also contains a duplicate.
	err := d.SetFindingRefs(fid, []FindingRef{
		{Scheme: SchemeASVS, Ref: "V1.1.1"},
		{Scheme: SchemeWSTG, Ref: "WSTG-INFO-01"},
		{Scheme: SchemeASVS, Ref: "V1.1.1"}, // duplicate collapses
	})
	if err != nil {
		t.Fatalf("SetFindingRefs: %v", err)
	}
	got, err := d.ListFindingRefs(fid)
	if err != nil {
		t.Fatalf("ListFindingRefs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("after replace want 2 rows, got %d", len(got))
	}
	for _, r := range got {
		if r.Scheme == SchemeCWE {
			t.Fatal("old CWE ref survived the replace")
		}
	}

	// An invalid entry rejects the whole set without mutating existing rows.
	if err := d.SetFindingRefs(fid, []FindingRef{{Scheme: "bogus", Ref: "x"}}); err == nil {
		t.Fatal("SetFindingRefs with bad entry = nil error, want rejection")
	}
	got, err = d.ListFindingRefs(fid)
	if err != nil {
		t.Fatalf("ListFindingRefs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("failed SetFindingRefs changed the set: got %d rows, want 2", len(got))
	}

	// An empty set clears all refs.
	if err := d.SetFindingRefs(fid, nil); err != nil {
		t.Fatalf("SetFindingRefs(nil): %v", err)
	}
	got, err = d.ListFindingRefs(fid)
	if err != nil {
		t.Fatalf("ListFindingRefs: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("after clearing want 0 rows, got %d", len(got))
	}
}

// TestDeleteFindingRef removes one ref by id and leaves the rest.
func TestDeleteFindingRef(t *testing.T) {
	d, fid := newRefFinding(t)

	id1, err := d.AddFindingRef(fid, SchemeCWE, "CWE-79")
	if err != nil {
		t.Fatalf("AddFindingRef: %v", err)
	}
	if _, err := d.AddFindingRef(fid, SchemeASVS, "V1.1.1"); err != nil {
		t.Fatalf("AddFindingRef: %v", err)
	}

	if err := d.DeleteFindingRef(id1); err != nil {
		t.Fatalf("DeleteFindingRef: %v", err)
	}
	got, err := d.ListFindingRefs(fid)
	if err != nil {
		t.Fatalf("ListFindingRefs: %v", err)
	}
	if len(got) != 1 || got[0].Scheme != SchemeASVS {
		t.Fatalf("after delete want only the ASVS ref, got %+v", got)
	}

	// Deleting a missing id is a no-op.
	if err := d.DeleteFindingRef(id1); err != nil {
		t.Fatalf("DeleteFindingRef on missing id: %v", err)
	}
}

// TestFindingRefCascade checks deleting the finding removes its refs.
func TestFindingRefCascade(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	t.Cleanup(func() { d.Close() })
	tk, err := d.CreateTask("finding refs cascade test", "Goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.DeleteTask(tk.ID) })
	fid, err := d.AddFinding(tk.ID, 0, "XSS", "Reflected XSS", "high", "s", "poc", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := d.AddFindingRef(fid, SchemeCWE, "CWE-79"); err != nil {
		t.Fatalf("AddFindingRef: %v", err)
	}

	if n, err := d.DeleteFinding(fid); err != nil || n != 1 {
		t.Fatalf("DeleteFinding: n=%d err=%v", n, err)
	}
	got, err := d.ListFindingRefs(fid)
	if err != nil {
		t.Fatalf("ListFindingRefs: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("refs survived finding deletion: got %d rows, want 0", len(got))
	}
}
