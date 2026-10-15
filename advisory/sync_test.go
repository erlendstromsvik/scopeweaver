package advisory

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// readVendored loads the vendored fixtures used as sync server bodies.
func readVendored(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func TestSyncEmptyRegisterAppendsAll(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "nsm.jsonl")
	rss := readVendored(t, vendoredNSM)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua != "ScopeWeaver-advisory-sync" {
			t.Errorf("User-Agent = %q, want ScopeWeaver-advisory-sync", ua)
		}
		w.Write(rss)
	}))
	defer srv.Close()

	res, err := Sync(t.Context(), SyncOptions{
		NSMRSSURL:    srv.URL,
		SkipKEV:      true,
		RegisterPath: reg,
		KEVPath:      filepath.Join(dir, "kev.json"),
		Seen:         time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.NSMAdded != 10 || res.NSMTotal != 10 {
		t.Fatalf("NSM added/total = %d/%d, want 10/10", res.NSMAdded, res.NSMTotal)
	}
	entries, err := ReadRegister(bytes.NewReader(readVendored(t, reg)))
	if err != nil {
		t.Fatalf("ReadRegister: %v", err)
	}
	if len(entries) != 10 {
		t.Fatalf("register has %d entries, want 10", len(entries))
	}
	for _, e := range entries {
		if e.FirstSeen != "2026-10-15" {
			t.Fatalf("entry %s FirstSeen = %q, want 2026-10-15", e.ID, e.FirstSeen)
		}
	}

	// A second sync with the same feed adds nothing.
	res2, err := Sync(t.Context(), SyncOptions{
		NSMRSSURL:    srv.URL,
		SkipKEV:      true,
		RegisterPath: reg,
		KEVPath:      filepath.Join(dir, "kev.json"),
		Seen:         time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if res2.NSMAdded != 0 || res2.NSMTotal != 10 {
		t.Fatalf("second sync added/total = %d/%d, want 0/10", res2.NSMAdded, res2.NSMTotal)
	}
}

func TestSyncPreSeededRegisterAddsRest(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "nsm.jsonl")
	rss := readVendored(t, vendoredNSM)
	recs, err := ParseNSMRSS(rss)
	if err != nil {
		t.Fatalf("ParseNSMRSS: %v", err)
	}
	// Seed three of the ten ids into the register file.
	seed := MergeRegister(nil, recs[:3], time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))
	var buf bytes.Buffer
	if err := AppendRegister(&buf, seed); err != nil {
		t.Fatalf("AppendRegister: %v", err)
	}
	if err := os.WriteFile(reg, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(rss) }))
	defer srv.Close()

	res, err := Sync(t.Context(), SyncOptions{
		NSMRSSURL:    srv.URL,
		SkipKEV:      true,
		RegisterPath: reg,
		KEVPath:      filepath.Join(dir, "kev.json"),
		Seen:         time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.NSMAdded != 7 || res.NSMTotal != 10 {
		t.Fatalf("added/total = %d/%d, want 7/10", res.NSMAdded, res.NSMTotal)
	}
}

func TestSyncNSMServerErrorLeavesRegisterUnchanged(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "nsm.jsonl")
	seed := `{"id":"a","source":"nsm","title":"A","date":"2026-01-01","link":"https://nsm.no/a","cves":[],"summary":"s","first_seen":"2026-01-02"}` + "\n"
	if err := os.WriteFile(reg, []byte(seed), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	before := []byte(seed)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := Sync(t.Context(), SyncOptions{
		NSMRSSURL:    srv.URL,
		SkipKEV:      true,
		RegisterPath: reg,
		KEVPath:      filepath.Join(dir, "kev.json"),
		Seen:         time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("Sync with 500 RSS: want error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error %q should carry the status", err)
	}
	after := readVendored(t, reg)
	if !bytes.Equal(before, after) {
		t.Fatalf("register changed on server error:\nbefore %q\nafter  %q", before, after)
	}
}

func TestSyncKEVUnchangedVersion(t *testing.T) {
	dir := t.TempDir()
	kevPath := filepath.Join(dir, "kev.json")
	vendored := readVendored(t, vendoredKEV)
	if err := os.WriteFile(kevPath, vendored, 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(vendored) }))
	defer srv.Close()

	res, err := Sync(t.Context(), SyncOptions{
		SkipNSM:      true,
		KEVURL:       srv.URL,
		RegisterPath: filepath.Join(dir, "nsm.jsonl"),
		KEVPath:      kevPath,
		Seen:         time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.KEVUpdated {
		t.Fatal("KEVUpdated = true, want false for same catalogVersion")
	}
	if res.KEVVersion != "2026.10.14" {
		t.Fatalf("KEVVersion = %q, want 2026.10.14", res.KEVVersion)
	}
	if res.KEVCount != 1745 {
		t.Fatalf("KEVCount = %d, want 1745", res.KEVCount)
	}
	if !bytes.Equal(vendored, readVendored(t, kevPath)) {
		t.Fatal("snapshot bytes changed on unchanged version")
	}
}

func TestSyncKEVNewVersionRewritesSnapshot(t *testing.T) {
	dir := t.TempDir()
	kevPath := filepath.Join(dir, "kev.json")
	vendored := readVendored(t, vendoredKEV)
	if err := os.WriteFile(kevPath, vendored, 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	// A modified copy with a new catalogVersion.
	updated := bytes.Replace(vendored, []byte(`"catalogVersion": "2026.10.14"`),
		[]byte(`"catalogVersion": "2026.10.15"`), 1)
	if bytes.Equal(updated, vendored) {
		t.Fatal("test fixture: version replace was a no-op")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(updated) }))
	defer srv.Close()

	res, err := Sync(t.Context(), SyncOptions{
		SkipNSM:      true,
		KEVURL:       srv.URL,
		RegisterPath: filepath.Join(dir, "nsm.jsonl"),
		KEVPath:      kevPath,
		Seen:         time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !res.KEVUpdated {
		t.Fatal("KEVUpdated = false, want true for new version")
	}
	if res.KEVVersion != "2026.10.15" {
		t.Fatalf("KEVVersion = %q, want 2026.10.15", res.KEVVersion)
	}
	now := readVendored(t, kevPath)
	if !bytes.Contains(now, []byte(`"catalogVersion": "2026.10.15"`)) {
		t.Fatal("snapshot does not carry the new version")
	}
	// No stray temp files left behind.
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".kev-") {
			t.Fatalf("leftover temp file %s", e.Name())
		}
	}
}

func TestSyncKEVInvalidLeavesSnapshotIntact(t *testing.T) {
	dir := t.TempDir()
	kevPath := filepath.Join(dir, "kev.json")
	vendored := readVendored(t, vendoredKEV)
	if err := os.WriteFile(kevPath, vendored, 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"title":"broken`))
	}))
	defer srv.Close()

	_, err := Sync(t.Context(), SyncOptions{
		SkipNSM:      true,
		KEVURL:       srv.URL,
		RegisterPath: filepath.Join(dir, "nsm.jsonl"),
		KEVPath:      kevPath,
		Seen:         time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("Sync with invalid KEV: want error, got nil")
	}
	if !bytes.Equal(vendored, readVendored(t, kevPath)) {
		t.Fatal("invalid KEV clobbered the snapshot")
	}
}

func TestSyncKEVCountMismatchLeavesSnapshotIntact(t *testing.T) {
	dir := t.TempDir()
	kevPath := filepath.Join(dir, "kev.json")
	vendored := readVendored(t, vendoredKEV)
	if err := os.WriteFile(kevPath, vendored, 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	// Same version marker but a lying count field: must not be written.
	bad := bytes.Replace(vendored, []byte(`"count": 1745`), []byte(`"count": 9999`), 1)
	if bytes.Equal(bad, vendored) {
		t.Fatal("test fixture: count replace was a no-op")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(bad) }))
	defer srv.Close()

	_, err := Sync(t.Context(), SyncOptions{
		SkipNSM:      true,
		KEVURL:       srv.URL,
		RegisterPath: filepath.Join(dir, "nsm.jsonl"),
		KEVPath:      kevPath,
		Seen:         time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("Sync with count mismatch: want error, got nil")
	}
	if !bytes.Equal(vendored, readVendored(t, kevPath)) {
		t.Fatal("count-mismatch KEV clobbered the snapshot")
	}
}

func TestSyncMissingRegisterFileIsTreatedAsEmpty(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "absent.jsonl")
	rss := readVendored(t, vendoredNSM)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(rss) }))
	defer srv.Close()

	res, err := Sync(t.Context(), SyncOptions{
		NSMRSSURL:    srv.URL,
		SkipKEV:      true,
		RegisterPath: reg,
		KEVPath:      filepath.Join(dir, "kev.json"),
		Seen:         time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Sync with missing register: %v", err)
	}
	if res.NSMAdded != 10 {
		t.Fatalf("added = %d, want 10", res.NSMAdded)
	}
	// File now exists with 10 lines.
	if lines := strings.Count(string(readVendored(t, reg)), "\n"); lines != 10 {
		t.Fatalf("register has %d lines, want 10", lines)
	}
}

func TestSyncDefaultsApplied(t *testing.T) {
	// Zero-value options must fill in the documented defaults (checked via
	// the exported constants; no network is touched here).
	if DefaultNSMRSSURL != "https://nsm.no/rss/sarbarhetsvarsler-fra-nsm/" {
		t.Fatalf("DefaultNSMRSSURL = %q", DefaultNSMRSSURL)
	}
	if DefaultKEVURL != "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json" {
		t.Fatalf("DefaultKEVURL = %q", DefaultKEVURL)
	}
	if DefaultRegisterPath != "advisory/register/nsm.jsonl" {
		t.Fatalf("DefaultRegisterPath = %q", DefaultRegisterPath)
	}
	if DefaultKEVPath != "standards/sources/cisa-kev/known_exploited_vulnerabilities.json" {
		t.Fatalf("DefaultKEVPath = %q", DefaultKEVPath)
	}
}

// Both sources run; the KEV server fails with 500. The NSM entries must
// still be appended, and the error must mention KEV — an NSM failure must
// never be reported for a KEV-only outage, and vice versa.
func TestSyncKEVServer500StillAppendsNSM(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "nsm.jsonl")
	rss := readVendored(t, vendoredNSM)
	nsmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(rss) }))
	defer nsmSrv.Close()
	kevSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer kevSrv.Close()

	res, err := Sync(t.Context(), SyncOptions{
		NSMRSSURL:    nsmSrv.URL,
		KEVURL:       kevSrv.URL,
		RegisterPath: reg,
		KEVPath:      filepath.Join(dir, "kev.json"),
		Seen:         time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	})
	if err == nil || !strings.Contains(err.Error(), "KEV") {
		t.Fatalf("want a KEV error, got %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error %q should carry the status", err)
	}
	if res.NSMAdded != 10 || res.NSMTotal != 10 {
		t.Fatalf("NSM added/total = %d/%d, want 10/10 despite the KEV failure", res.NSMAdded, res.NSMTotal)
	}
	if lines := strings.Count(string(readVendored(t, reg)), "\n"); lines != 10 {
		t.Fatalf("register has %d lines, want 10", lines)
	}
	if _, err := os.Stat(filepath.Join(dir, "kev.json")); !os.IsNotExist(err) {
		t.Fatalf("KEV snapshot created despite the 500: %v", err)
	}
}

// The mirror case: NSM fails, KEV still updates.
func TestSyncNSMFailureStillUpdatesKEV(t *testing.T) {
	dir := t.TempDir()
	kevPath := filepath.Join(dir, "kev.json")
	vendored := readVendored(t, vendoredKEV)
	nsmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer nsmSrv.Close()
	kevSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(vendored) }))
	defer kevSrv.Close()

	res, err := Sync(t.Context(), SyncOptions{
		NSMRSSURL:    nsmSrv.URL,
		KEVURL:       kevSrv.URL,
		RegisterPath: filepath.Join(dir, "nsm.jsonl"),
		KEVPath:      kevPath,
		Seen:         time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	})
	if err == nil || !strings.Contains(err.Error(), "NSM") {
		t.Fatalf("want an NSM error, got %v", err)
	}
	if !res.KEVUpdated {
		t.Fatal("KEVUpdated = false, want true despite the NSM failure")
	}
	if res.KEVVersion != "2026.10.14" || res.KEVCount != 1745 {
		t.Fatalf("KEV version/count = %q/%d, want 2026.10.14/1745", res.KEVVersion, res.KEVCount)
	}
	if !bytes.Equal(vendored, readVendored(t, kevPath)) {
		t.Fatal("KEV snapshot bytes differ from the served catalog")
	}
	if _, err := os.Stat(filepath.Join(dir, "nsm.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("register created despite the NSM failure: %v", err)
	}
}

// Guard: the register written by Sync round-trips through ReadRegister and
// the JSONL stays unescaped (titles with & and < survive).
func TestSyncRegisterRoundTrip(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "nsm.jsonl")
	rss := readVendored(t, vendoredNSM)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(rss) }))
	defer srv.Close()
	if _, err := Sync(t.Context(), SyncOptions{
		NSMRSSURL:    srv.URL,
		SkipKEV:      true,
		RegisterPath: reg,
		KEVPath:      filepath.Join(dir, "kev.json"),
		Seen:         time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	raw := readVendored(t, reg)
	if bytes.Contains(raw, []byte("\\u0026")) || bytes.Contains(raw, []byte("\\u003c")) {
		t.Fatal("register HTML-escaped ampersands/lt")
	}
	entries, err := ReadRegister(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadRegister: %v", err)
	}
	if len(entries) != 10 {
		t.Fatalf("entries = %d, want 10", len(entries))
	}
	// Every entry is a single compact JSON line.
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		var e RegisterEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("line not compact JSON: %v", err)
		}
	}
}
