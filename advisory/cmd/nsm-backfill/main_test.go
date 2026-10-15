package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunBackfillTwiceAddsOnce(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "nsm.jsonl")
	seen := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	added, present, total, err := run(reg, seen, []string{"../../testdata/nsm-archive-page-00.html"})
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if added != 10 || present != 0 || total != 10 {
		t.Errorf("first run: added=%d present=%d total=%d, want 10/0/10", added, present, total)
	}
	if lines := countLines(t, reg); lines != 10 {
		t.Errorf("register has %d lines after first run, want 10", lines)
	}
	added, present, total, err = run(reg, seen, []string{"../../testdata/nsm-archive-page-00.html"})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if added != 0 || present != 10 || total != 10 {
		t.Errorf("second run: added=%d present=%d total=%d, want 0/10/10", added, present, total)
	}
	if lines := countLines(t, reg); lines != 10 {
		t.Errorf("register has %d lines after second run, want 10", lines)
	}
}

func TestRunBackfillMissingPageFile(t *testing.T) {
	dir := t.TempDir()
	if _, _, _, err := run(filepath.Join(dir, "nsm.jsonl"), time.Unix(0, 0), []string{filepath.Join(dir, "nope.html")}); err == nil {
		t.Fatal("missing page file must error")
	}
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return len(bytes.Split(bytes.TrimSpace(b), []byte("\n")))
}
