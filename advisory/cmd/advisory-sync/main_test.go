package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// The command wrapper: run() against httptest servers with URL flags.
func TestCommandRun(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "nsm.jsonl")
	kevPath := filepath.Join(dir, "kev.json")
	rss := readFixture(t, "../../testdata/nsm-sarbarhetsvarsler-2026-10-15.rss")
	kevBytes := readFixture(t, "../../../standards/sources/cisa-kev/known_exploited_vulnerabilities.json")
	nsmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(rss) }))
	defer nsmSrv.Close()
	kevSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(kevBytes) }))
	defer kevSrv.Close()

	var out bytes.Buffer
	err := run([]string{
		"-register", reg,
		"-kev", kevPath,
		"-nsm-url", nsmSrv.URL,
		"-kev-url", kevSrv.URL,
		"-seen", "2026-10-15",
	}, &out)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "NSM: added 10 (total 10)\nKEV: catalog 2026.10.14, 1745 entries, updated\n"
	if out.String() != want {
		t.Fatalf("stdout = %q, want %q", out.String(), want)
	}
	// Second run: nothing new, KEV unchanged.
	out.Reset()
	if err := run([]string{
		"-register", reg,
		"-kev", kevPath,
		"-nsm-url", nsmSrv.URL,
		"-kev-url", kevSrv.URL,
		"-seen", "2026-10-16",
	}, &out); err != nil {
		t.Fatalf("second run: %v", err)
	}
	want2 := "NSM: added 0 (total 10)\nKEV: catalog 2026.10.14, 1745 entries, unchanged\n"
	if out.String() != want2 {
		t.Fatalf("stdout = %q, want %q", out.String(), want2)
	}
}

// -skip-kev: only the NSM line prints; the KEV file is never touched.
func TestCommandRunSkipKEV(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "nsm.jsonl")
	kevPath := filepath.Join(dir, "kev.json")
	rss := readFixture(t, "../../testdata/nsm-sarbarhetsvarsler-2026-10-15.rss")
	nsmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(rss) }))
	defer nsmSrv.Close()
	kevSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("KEV server hit despite -skip-kev")
	}))
	defer kevSrv.Close()

	var out bytes.Buffer
	if err := run([]string{
		"-register", reg,
		"-kev", kevPath,
		"-nsm-url", nsmSrv.URL,
		"-kev-url", kevSrv.URL,
		"-seen", "2026-10-15",
		"-skip-kev",
	}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "NSM: added 10 (total 10)\n" {
		t.Fatalf("stdout = %q, want only the NSM line", out.String())
	}
	if _, err := os.Stat(kevPath); !os.IsNotExist(err) {
		t.Fatalf("KEV snapshot created despite -skip-kev: %v", err)
	}
}

// -skip-nsm: only the KEV line prints; the register file is never created.
func TestCommandRunSkipNSM(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "nsm.jsonl")
	kevPath := filepath.Join(dir, "kev.json")
	kevBytes := readFixture(t, "../../../standards/sources/cisa-kev/known_exploited_vulnerabilities.json")
	nsmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("NSM server hit despite -skip-nsm")
	}))
	defer nsmSrv.Close()
	kevSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(kevBytes) }))
	defer kevSrv.Close()

	var out bytes.Buffer
	if err := run([]string{
		"-register", reg,
		"-kev", kevPath,
		"-nsm-url", nsmSrv.URL,
		"-kev-url", kevSrv.URL,
		"-seen", "2026-10-15",
		"-skip-nsm",
	}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "KEV: catalog 2026.10.14, 1745 entries, updated\n" {
		t.Fatalf("stdout = %q, want only the KEV line", out.String())
	}
	if _, err := os.Stat(reg); !os.IsNotExist(err) {
		t.Fatalf("register created despite -skip-nsm: %v", err)
	}
}

// One source failing must not stop the other: NSM syncs, KEV 500s, the
// error is returned (exit 1) after the NSM line is printed.
func TestCommandRunKEVFailsNSMSucceeds(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "nsm.jsonl")
	rss := readFixture(t, "../../testdata/nsm-sarbarhetsvarsler-2026-10-15.rss")
	nsmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(rss) }))
	defer nsmSrv.Close()
	kevSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer kevSrv.Close()

	var out bytes.Buffer
	err := run([]string{
		"-register", reg,
		"-kev", filepath.Join(dir, "kev.json"),
		"-nsm-url", nsmSrv.URL,
		"-kev-url", kevSrv.URL,
		"-seen", "2026-10-15",
	}, &out)
	if err == nil || !strings.Contains(err.Error(), "KEV") {
		t.Fatalf("want KEV error, got %v", err)
	}
	if !strings.HasPrefix(out.String(), "NSM: added 10 (total 10)\n") {
		t.Fatalf("stdout = %q, want the NSM success line first", out.String())
	}
	if lines := strings.Count(string(readFixture(t, reg)), "\n"); lines != 10 {
		t.Fatalf("register has %d lines, want 10", lines)
	}
}

func TestCommandRunBadSeen(t *testing.T) {
	err := run([]string{"-seen", "not-a-date"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Fatalf("want -seen error, got %v", err)
	}
}
