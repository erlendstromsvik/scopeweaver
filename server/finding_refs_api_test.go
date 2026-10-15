package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/dbtest"
)

func TestResolveFindingRef(t *testing.T) {
	for _, tt := range []struct {
		name    string
		scheme  string
		ref     string
		wantErr bool
	}{
		{"asvs hit", db.SchemeASVS, "V1.1.1", false},
		{"asvs miss", db.SchemeASVS, "V99.99.99", true},
		{"wstg hit", db.SchemeWSTG, "WSTG-INFO-01", false},
		{"wstg miss", db.SchemeWSTG, "WSTG-NOPE-99", true},
		{"top10 hit", db.SchemeOWASPTop10, "A01:2025", false},
		{"top10 miss", db.SchemeOWASPTop10, "A99:2025", true},
		{"cwe shape-only", db.SchemeCWE, "CWE-79", false},
		{"cvss shape-only", db.SchemeCVSS, "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", false},
		{"unknown scheme", "nope", "whatever", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := resolveFindingRef(tt.scheme, tt.ref)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveFindingRef(%q, %q) err=%v, wantErr=%v", tt.scheme, tt.ref, err, tt.wantErr)
			}
		})
	}
}

// refsFixture creates a task with one finding and returns a request helper bound
// to a fully constructed, JWT-authenticated Server (same shape as
// trafficEvidenceServer).
func refsFixture(t *testing.T) (func(method, path, body string) *httptest.ResponseRecorder, int64) {
	t.Helper()
	dbtest.DSN(t) // skip unless ARTEX_TEST_DSN points at a throwaway DB
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	task, err := m.CreateTask("refs api", "goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	tid, err := strconv.ParseInt(task.ID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.pg.Exec(`DELETE FROM task_archives WHERE task_id=$1`, tid); m.pg.DeleteTask(tid) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := New(ctx, m, t.TempDir(), t.TempDir(), t.TempDir())
	s.archiveWG.Wait()
	// Wait for the side-question snapshot writer before reusing this fixture.
	<-s.side.done
	token, err := signJWT(s.jwtKey)
	if err != nil {
		t.Fatal(err)
	}
	fid, err := m.pg.AddFinding(tid, 0, "TEST", "refs finding", "low", "summary", "", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	return request, fid
}

func decodeRefs(t *testing.T, w *httptest.ResponseRecorder) []FindingRefDTO {
	t.Helper()
	var out struct {
		Refs []FindingRefDTO `json:"refs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return out.Refs
}

func TestFindingRefsAPI(t *testing.T) {
	request, fid := refsFixture(t)
	path := fmt.Sprintf("/api/exploration/findings/%d/refs", fid)

	// GET on a fresh finding: empty list, never null.
	w := request(http.MethodGet, path, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET code=%d body=%s", w.Code, w.Body.String())
	}
	if got := decodeRefs(t, w); len(got) != 0 || got == nil {
		t.Fatalf("fresh refs = %+v, want empty non-nil", got)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"refs":[]`)) {
		t.Fatalf("fresh body = %s, want \"refs\":[]", w.Body.String())
	}

	// PUT two valid refs: 200 and both come back.
	w = request(http.MethodPut, path, `{"refs":[{"scheme":"cwe","ref":"CWE-79"},{"scheme":"asvs","ref":"V1.1.1"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT code=%d body=%s", w.Code, w.Body.String())
	}
	got := decodeRefs(t, w)
	if len(got) != 2 || got[0] != (FindingRefDTO{Scheme: "asvs", Ref: "V1.1.1"}) || got[1] != (FindingRefDTO{Scheme: "cwe", Ref: "CWE-79"}) {
		t.Fatalf("PUT refs = %+v", got)
	}
	w = request(http.MethodGet, path, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET after PUT code=%d body=%s", w.Code, w.Body.String())
	}
	if got := decodeRefs(t, w); len(got) != 2 {
		t.Fatalf("GET after PUT refs = %+v, want 2", got)
	}

	// PUT with an unknown asvs ref: 400 and the previous refs survive.
	w = request(http.MethodPut, path, `{"refs":[{"scheme":"asvs","ref":"V99.99.99"}]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad-ref PUT code=%d body=%s", w.Code, w.Body.String())
	}
	w = request(http.MethodGet, path, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET after bad PUT code=%d body=%s", w.Code, w.Body.String())
	}
	if got := decodeRefs(t, w); len(got) != 2 || got[0].Scheme != "asvs" || got[1].Scheme != "cwe" {
		t.Fatalf("refs after bad PUT = %+v, want the previous two", got)
	}

	// Unknown finding id: 404 on both verbs.
	missing := "/api/exploration/findings/999999999/refs"
	if w := request(http.MethodGet, missing, ""); w.Code != http.StatusNotFound {
		t.Fatalf("GET missing code=%d body=%s", w.Code, w.Body.String())
	}
	if w := request(http.MethodPut, missing, `{"refs":[]}`); w.Code != http.StatusNotFound {
		t.Fatalf("PUT missing code=%d body=%s", w.Code, w.Body.String())
	}
}
