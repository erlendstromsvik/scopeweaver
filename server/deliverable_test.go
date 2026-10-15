package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

func TestDeliverableInstanceID(t *testing.T) {
	long := strings.Repeat("a", 70)
	cases := []struct {
		name     string
		env      string // "" means unset
		fallback string
		want     string
	}{
		{"env wins", "sw-42", "bp", "sw-42"},
		{"fallback to blueprint", "", "karriereveiledning", "karriereveiledning"},
		{"sanitize", "sw/42:prod!", "x", "sw-42-prod"},
		{"dots underscores kept", "sw_42.v1", "x", "sw_42.v1"},
		{"empty env and name", "", "", "instance"},
		{"all delimiters", "---", "x", "instance"},
		{"trim edges", "..sw-1..", "x", "sw-1"},
		{"cap 64", long, "x", strings.Repeat("a", 64)},
		{"cap trims to delimiter", strings.Repeat("b", 63) + "-", "x", strings.Repeat("b", 63)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env == "" {
				t.Setenv("SCOPEWEAVER_INSTANCE_ID", "")
			} else {
				t.Setenv("SCOPEWEAVER_INSTANCE_ID", tc.env)
			}
			if got := deliverableInstanceID(tc.fallback); got != tc.want {
				t.Errorf("deliverableInstanceID(%q) = %q, want %q", tc.fallback, got, tc.want)
			}
		})
	}
}

// deliverableFixture builds a provisioned blueprint instance with one finding
// and one task-scoped asset, the task terminal, and a fresh deliverable dir.
// Returns the server, the request helper, the deliverable dir, the task id and
// the finding name.
func deliverableFixture(t *testing.T) (*Server, func(method, path, body string) *httptest.ResponseRecorder, string, int64, string) {
	t.Helper()
	s, request := instanceStatusTestServer(t)
	cleanupBlueprintSettings(t, s)
	pg := s.m.PG()

	task, err := s.m.CreateTask("deliverable task", "deliverable goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := strconv.ParseInt(task.ID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteTask(taskID) })
	if err := pg.SetSetting(blueprintProvisionedKey, "bp-deliv"); err != nil {
		t.Fatal(err)
	}
	if err := pg.SetSetting(blueprintTaskIDKey, task.ID); err != nil {
		t.Fatal(err)
	}

	findingName := fmt.Sprintf("deliverable-finding-%d", time.Now().UnixNano()%1000000)
	fid, err := pg.AddFinding(taskID, 0, "TEST", findingName, "high", "summary", "evidence", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pg.Exec(`DELETE FROM findings WHERE id = $1`, fid) })

	stamp := time.Now().UnixNano() % 1000000
	rootDomain := fmt.Sprintf("deliv-%d.example", stamp)
	assetID, err := s.m.Assets().UpsertRootDomain(db.UpsertRootDomainReq{Domain: rootDomain, TaskID: taskID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pg.Exec(`DELETE FROM assets WHERE id = $1`, assetID) })

	if err := pg.SetStatus(taskID, "done"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s.SetDeliverableDir(dir)
	return s, request, dir, taskID, findingName
}

func TestDeliverableBundleContents(t *testing.T) {
	_, request, dir, taskID, findingName := deliverableFixture(t)

	w := request("POST", "/api/instance/deliverable", "")
	if w.Code != 200 {
		t.Fatalf("POST: got %d, want 200 (%s)", w.Code, w.Body)
	}
	var resp struct {
		Dir      string              `json:"dir"`
		Manifest deliverableManifest `json:"manifest"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %s: %v", w.Body, err)
	}
	if resp.Dir == "" || strings.Contains(resp.Dir, "/") {
		t.Fatalf("dir should be basename only, got %q", resp.Dir)
	}
	if resp.Manifest.Instance != "bp-deliv" || resp.Manifest.TaskID != strconv.FormatInt(taskID, 10) || resp.Manifest.Status != "done" {
		t.Fatalf("manifest identity: %+v", resp.Manifest)
	}
	if _, err := time.Parse(time.RFC3339, resp.Manifest.CreatedAt); err != nil {
		t.Fatalf("created_at not RFC3339: %q (%v)", resp.Manifest.CreatedAt, err)
	}
	wantNames := []string{"findings.md", "findings.csv", "findings-evidence.zip", "assets.csv"}
	if len(resp.Manifest.Files) != len(wantNames) {
		t.Fatalf("manifest files = %d, want %d: %+v", len(resp.Manifest.Files), len(wantNames), resp.Manifest.Files)
	}
	for i, f := range resp.Manifest.Files {
		if f.Name != wantNames[i] {
			t.Errorf("file %d = %q, want %q", i, f.Name, wantNames[i])
		}
	}

	// The bundle dir exists with exactly the 5 files; every manifest entry
	// matches the file on disk (size + sha256).
	bundle := filepath.Join(dir, resp.Dir)
	entries, err := os.ReadDir(bundle)
	if err != nil {
		t.Fatalf("bundle dir: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("bundle has %d entries, want 5: %v", len(entries), entries)
	}
	for _, f := range resp.Manifest.Files {
		data, err := os.ReadFile(filepath.Join(bundle, f.Name))
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		if int64(len(data)) != f.Size {
			t.Errorf("%s size = %d, manifest %d", f.Name, len(data), f.Size)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != f.SHA256 {
			t.Errorf("%s sha256 mismatch: disk %s, manifest %s", f.Name, hex.EncodeToString(sum[:]), f.SHA256)
		}
	}
	// findings.csv carries the seeded finding's name.
	csvData, err := os.ReadFile(filepath.Join(bundle, "findings.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(csvData), findingName) {
		t.Errorf("findings.csv missing finding name %q", findingName)
	}
	// No partial dirs left behind.
	assertNoPartials(t, dir)
}

func TestDeliverableSecondRunFreshDir(t *testing.T) {
	s, request, dir, _, _ := deliverableFixture(t)

	w := request("POST", "/api/instance/deliverable", "")
	if w.Code != 200 {
		t.Fatalf("first POST: got %d (%s)", w.Code, w.Body)
	}
	var first struct {
		Dir string `json:"dir"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	// A later `now` yields a different timestamped dir.
	now := time.Now().Add(2 * time.Second)
	dest, m, err := s.writeDeliverable(now)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if filepath.Base(dest) == first.Dir {
		t.Fatalf("second bundle reused dir %q", first.Dir)
	}
	if m.Instance != "bp-deliv" {
		t.Fatalf("second manifest instance %q", m.Instance)
	}
	// Both bundles complete; no partials.
	for _, d := range []string{first.Dir, filepath.Base(dest)} {
		entries, err := os.ReadDir(filepath.Join(dir, d))
		if err != nil {
			t.Fatalf("bundle %s: %v", d, err)
		}
		if len(entries) != 5 {
			t.Errorf("bundle %s has %d entries, want 5", d, len(entries))
		}
	}
	assertNoPartials(t, dir)
}

func TestDeliverableConflicts(t *testing.T) {
	// dir not configured
	s, request, _, _, _ := deliverableFixture(t)
	s.SetDeliverableDir("")
	w := request("POST", "/api/instance/deliverable", "")
	if w.Code != 409 {
		t.Fatalf("no dir: got %d, want 409 (%s)", w.Code, w.Body)
	}

	// not provisioned
	s2, request2, dir2, _, _ := deliverableFixture(t)
	if _, err := s2.m.PG().Exec(`DELETE FROM settings WHERE key = $1`, blueprintProvisionedKey); err != nil {
		t.Fatal(err)
	}
	w = request2("POST", "/api/instance/deliverable", "")
	if w.Code != 409 {
		t.Fatalf("not provisioned: got %d, want 409 (%s)", w.Code, w.Body)
	}
	assertNoPartials(t, dir2)

	// task not terminal
	s3, request3, dir3, taskID, _ := deliverableFixture(t)
	if err := s3.m.PG().SetStatus(taskID, "running"); err != nil {
		t.Fatal(err)
	}
	w = request3("POST", "/api/instance/deliverable", "")
	if w.Code != 409 {
		t.Fatalf("running task: got %d, want 409 (%s)", w.Code, w.Body)
	}
	assertNoPartials(t, dir3)
}

func TestDeliverableUnauthenticated(t *testing.T) {
	s, _, _, _, _ := deliverableFixture(t)
	r := httptest.NewRequest("POST", "/api/instance/deliverable", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("unauthenticated: got %d, want 401", w.Code)
	}
}

// assertNoPartials fails if any ".partial-*" entry remains in dir.
func assertNoPartials(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read deliverable dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".partial-") {
			t.Errorf("partial dir left behind: %s", e.Name())
		}
	}
}
