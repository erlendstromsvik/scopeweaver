package server

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	pgdb "github.com/Autumn-27/artex/db"
)

// snapshotTaskArchive writes a full task-archive package (same format and
// payload layout as archiveTask, so the existing restore path can read it) to
// outPath WITHOUT touching the live task. archiveTask MOVES the task's files
// into staging and deletes the task afterwards; this is the copy-only variant
// the deliverable bundle needs: no progress updates, no completion, no traffic
// delete stage, no task deletion.
func (s *Server) snapshotTaskArchive(ctx context.Context, taskID int64, outPath string) error {
	task, err := s.m.pg.GetTask(taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return pgdb.ErrTaskArchiveNotFound
	}
	payload, err := os.MkdirTemp("", "artex-task-snapshot-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(payload)
	if err := os.MkdirAll(filepath.Join(payload, "files"), archiveDirMode); err != nil {
		return err
	}
	// Same sources as stageTaskArchiveFiles, copied instead of renamed.
	taskIDStr := strconv.FormatInt(taskID, 10)
	taskDir := filepath.Join(s.m.dir, "tasks", taskIDStr)
	if _, err := os.Lstat(taskDir); err == nil {
		if err := copyTree(taskDir, filepath.Join(payload, "files", "tasks", taskIDStr)); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	transcriptDir := filepath.Join(s.m.dir, "transcripts")
	entries, err := os.ReadDir(transcriptDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	prefix := "exp" + strconv.FormatInt(task.ExplorationID, 10) + "-"
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || (!entry.IsDir() && !strings.HasSuffix(name, ".jsonl")) {
			continue
		}
		source := filepath.Join(transcriptDir, name)
		destination := filepath.Join(payload, "files", "transcripts", name)
		if entry.IsDir() {
			if err := copyTree(source, destination); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destination), archiveDirMode); err != nil {
			return err
		}
		if err := copyFile(source, destination); err != nil {
			return err
		}
	}
	// Read-only database snapshot with the LLM records streamed to the same
	// payload path archiveTask uses.
	streamPath := filepath.Join(payload, filepath.FromSlash(pgdb.TaskArchiveLLMRecordsPath))
	if err := os.MkdirAll(filepath.Dir(streamPath), archiveDirMode); err != nil {
		return err
	}
	streamFile, err := os.OpenFile(streamPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, archiveFileMode)
	if err != nil {
		return err
	}
	snapshot, snapshotErr := s.m.pg.SnapshotTaskArchiveWithLLMRecords(taskID, streamFile)
	var syncErr error
	if snapshotErr == nil {
		syncErr = streamFile.Sync()
	}
	closeErr := streamFile.Close()
	if err := errors.Join(snapshotErr, syncErr, closeErr); err != nil {
		return err
	}
	if snapshot.ExplorationID != task.ExplorationID {
		return errors.New("the task exploration record changed while taking the archive snapshot")
	}
	if s.m.traffic != nil && len(snapshot.Hosts) > 0 {
		trafficCount, err := s.m.traffic.ExportHosts(snapshot.Hosts, filepath.Join(payload, "traffic"))
		if err != nil {
			return err
		}
		snapshot.DataCounts["traffic"] = trafficCount
	}
	evidenceSnapshots, err := pgdb.ArchiveEvidenceSnapshots(snapshot)
	if err != nil {
		return err
	}
	if err := s.evidenceStore().CopySnapshots(ctx, evidenceSnapshots, filepath.Join(payload, "evidence")); err != nil {
		return err
	}
	_, _, _, err = writeTaskArchivePackage(outPath, payload, snapshot)
	return err
}

// copyTree copies src into dst recursively: directories are created with
// archiveDirMode, regular files with archiveFileMode, symlinks are recreated as
// symlinks (never followed), and any other entry type is skipped.
func copyTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst)
	}
	if err := os.MkdirAll(dst, archiveDirMode); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		source := filepath.Join(src, entry.Name())
		destination := filepath.Join(dst, entry.Name())
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entryInfo.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(source)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(destination), archiveDirMode); err != nil {
				return err
			}
			if err := os.Symlink(target, destination); err != nil {
				return err
			}
		case entryInfo.IsDir():
			if err := copyTree(source, destination); err != nil {
				return err
			}
		case entryInfo.Mode().IsRegular():
			if err := copyFile(source, destination); err != nil {
				return err
			}
		default:
			continue
		}
	}
	return nil
}

// copyFile copies one regular file to dst with archiveFileMode.
func copyFile(src, dst string) error {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, archiveFileMode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}
