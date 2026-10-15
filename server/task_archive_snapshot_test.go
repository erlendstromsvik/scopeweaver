package server

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// listArchiveEntries returns every tar entry name (slash-separated) inside a
// .tar.zst package.
func listArchiveEntries(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()
	dec, err := zstd.NewReader(f)
	if err != nil {
		t.Fatalf("zstd reader: %v", err)
	}
	defer dec.Close()
	tr := tar.NewReader(dec)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		names = append(names, h.Name)
	}
	return names
}

// TestSnapshotTaskArchiveNonDestructive proves snapshotTaskArchive packages a
// full task archive without moving or deleting anything the live task owns.
func TestSnapshotTaskArchiveNonDestructive(t *testing.T) {
	s, _ := instanceStatusTestServer(t)
	pg := s.m.PG()

	task, err := s.m.CreateTask("snapshot task", "snapshot goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := strconv.ParseInt(task.ID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteTask(taskID) })

	findingName := "snapshot-finding"
	fid, err := pg.AddFinding(taskID, 0, "TEST", findingName, "high", "summary", "evidence", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pg.Exec(`DELETE FROM findings WHERE id = $1`, fid) })

	taskDir := filepath.Join(s.m.dir, "tasks", task.ID)
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(taskDir, "note.txt")
	const noteContent = "snapshot me"
	if err := os.WriteFile(notePath, []byte(noteContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pg.SetStatus(taskID, "done"); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "snapshot.tar.zst")
	if err := s.snapshotTaskArchive(context.Background(), taskID, out); err != nil {
		t.Fatalf("snapshotTaskArchive: %v", err)
	}

	// The live task and its files are untouched.
	after, err := pg.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if after == nil {
		t.Fatal("task vanished after snapshot")
	}
	got, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("note.txt moved away: %v", err)
	}
	if string(got) != noteContent {
		t.Errorf("note.txt content = %q, want %q", got, noteContent)
	}

	// The archive exists and carries the task file under the same payload
	// layout archiveTask produces (files/tasks/<id>/note.txt).
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("archive missing: %v", err)
	}
	wantEntry := "files/tasks/" + task.ID + "/note.txt"
	found := false
	for _, name := range listArchiveEntries(t, out) {
		if strings.HasSuffix(name, wantEntry) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("archive missing entry %q; entries: %v", wantEntry, listArchiveEntries(t, out))
	}
}
