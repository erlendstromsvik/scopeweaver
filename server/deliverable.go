package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/locale"
	"github.com/Autumn-27/artex/report"
)

// Deliverable preconditions. The handler maps each to 409 so an orchestrator
// can tell "not yet / not configured" from a real failure.
var (
	errDeliverableNoDir          = errors.New("deliverable directory is not configured")
	errDeliverableNotProvisioned = errors.New("instance is not provisioned; no deliverable")
	errDeliverableNoTask         = errors.New("blueprint task id is missing; no deliverable")
	errDeliverableNotTerminal    = errors.New("task is not terminal yet; deliverable unavailable")
	errDeliverableExists         = errors.New("deliverable already exists")
)

// deliverableInstanceID derives the bundle's instance name from the
// orchestrator-provided SCOPEWEAVER_INSTANCE_ID, falling back to the blueprint
// name. Anything outside [A-Za-z0-9._-] becomes '-', the result is capped at
// 64 chars, leading/trailing '-' and '.' are trimmed, and an empty result
// becomes "instance" so the bundle directory name is never empty.
func deliverableInstanceID(blueprintName string) string {
	raw := os.Getenv("SCOPEWEAVER_INSTANCE_ID")
	if raw == "" {
		raw = blueprintName
	}
	out := make([]rune, 0, len(raw))
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			out = append(out, r)
		default:
			out = append(out, '-')
		}
	}
	id := string(out)
	if len(id) > 64 {
		id = id[:64]
	}
	id = trimDelim(id)
	if id == "" {
		return "instance"
	}
	return id
}

// trimDelim strips leading/trailing '-' and '.' repeatedly, so a raw value
// that becomes all delimiters (or is truncated to a trailing '-') still
// yields a clean name.
func trimDelim(s string) string {
	for len(s) > 0 && (s[0] == '-' || s[0] == '.') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == '-' || s[len(s)-1] == '.') {
		s = s[:len(s)-1]
	}
	return s
}

// deliverableFile records one bundle member: its name inside the bundle
// directory, its size in bytes, and its SHA-256 hex digest.
type deliverableFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// deliverableManifest is the bundle's own inventory: which instance and task
// it came from, the task status at export time, when it was written, and a
// checksummed entry per file.
type deliverableManifest struct {
	Instance  string            `json:"instance"`
	TaskID    string            `json:"task_id"`
	Status    string            `json:"status"`
	CreatedAt string            `json:"created_at"`
	Files     []deliverableFile `json:"files"`
}

// SetDeliverableDir configures the host-mounted directory the deliverable
// bundle is written to. Called once at boot from cmd/artex; empty disables
// the export (POST /api/instance/deliverable then answers 409).
func (s *Server) SetDeliverableDir(dir string) {
	s.deliverableDir = dir
}

// writeDeliverable writes the blueprint run's deliverable bundle into
// s.deliverableDir and returns the final bundle directory plus its manifest.
//
// The bundle is staged in a ".partial-*" temp dir INSIDE the deliverable dir
// (same filesystem, so the rename is atomic) and renamed into
// <instance>-<UTC timestamp> only after every file, including manifest.json,
// is on disk — an observer sees a complete bundle or nothing. On any error
// the temp dir is removed. The target name must not already exist: two
// exports in the same second are refused rather than merged.
func (s *Server) writeDeliverable(ctx context.Context, now time.Time) (string, deliverableManifest, error) {
	if s.deliverableDir == "" {
		return "", deliverableManifest{}, errDeliverableNoDir
	}
	st, err := s.instanceStatusNow()
	if err != nil {
		return "", deliverableManifest{}, err
	}
	if !st.Provisioned {
		return "", deliverableManifest{}, errDeliverableNotProvisioned
	}
	if st.TaskID == "" {
		return "", deliverableManifest{}, errDeliverableNoTask
	}
	if !st.Terminal {
		return "", deliverableManifest{}, errDeliverableNotTerminal
	}
	lang := locale.ServerDefault()
	taskID, err := strconv.ParseInt(st.TaskID, 10, 64)
	if err != nil {
		return "", deliverableManifest{}, err
	}

	findings, err := s.m.pg.ListFindingsForExport(db.FindingFilter{TaskID: st.TaskID}, nil)
	if err != nil {
		return "", deliverableManifest{}, err
	}
	// Evidence staging mirrors the md-zip branch of findingsExport: freeze the
	// bindings and copy private bodies into a scratch dir, then zip from there.
	stage, err := os.MkdirTemp("", "artex-deliverable-evidence-")
	if err != nil {
		return "", deliverableManifest{}, err
	}
	defer os.RemoveAll(stage)
	if err := s.evidenceStore().StageFindingsExport(ctx, findings, stage, true); err != nil {
		return "", deliverableManifest{}, err
	}

	// tmp lives inside the deliverable dir so the final rename never crosses
	// filesystems (a host mount is a different fs from the container's /tmp).
	tmp, err := os.MkdirTemp(s.deliverableDir, ".partial-*")
	if err != nil {
		return "", deliverableManifest{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tmp)
		}
	}()

	var files []deliverableFile
	write := func(name string, data []byte) error {
		if err := os.WriteFile(filepath.Join(tmp, name), data, 0o644); err != nil {
			return err
		}
		sum, size, err := hashFile(filepath.Join(tmp, name))
		if err != nil {
			return err
		}
		files = append(files, deliverableFile{Name: name, Size: size, SHA256: sum})
		return nil
	}

	if err := write("findings.md", []byte(report.FindingsMarkdown(findings, now, lang))); err != nil {
		return "", deliverableManifest{}, err
	}
	if err := write("findings.csv", report.FindingsCSV(findings, lang)); err != nil {
		return "", deliverableManifest{}, err
	}
	zipPath := filepath.Join(tmp, "findings-evidence.zip")
	if err := buildFindingsEvidenceZip(zipPath, findings, stage, now, lang); err != nil {
		return "", deliverableManifest{}, err
	}
	zipSum, zipSize, err := hashFile(zipPath)
	if err != nil {
		return "", deliverableManifest{}, err
	}
	files = append(files, deliverableFile{Name: "findings-evidence.zip", Size: zipSize, SHA256: zipSum})

	// Assets: the store may be nil (asset coverage disabled); a headers-only
	// CSV keeps the bundle shape stable for the orchestrator.
	var assets []*db.Asset
	if as := s.assetStore(); as != nil {
		assets, err = as.ListAssetsForExport(db.AssetExportFilter{TaskID: taskID}, nil)
		if err != nil {
			return "", deliverableManifest{}, err
		}
	}
	if err := write("assets.csv", report.AssetsCSV(assets, lang)); err != nil {
		return "", deliverableManifest{}, err
	}

	m := deliverableManifest{
		Instance:  deliverableInstanceID(st.Blueprint),
		TaskID:    st.TaskID,
		Status:    st.Status,
		CreatedAt: now.UTC().Format(time.RFC3339),
		Files:     files,
	}
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", deliverableManifest{}, err
	}
	// manifest.json last: its presence completes the bundle.
	if err := os.WriteFile(filepath.Join(tmp, "manifest.json"), manifest, 0o644); err != nil {
		return "", deliverableManifest{}, err
	}

	dest := filepath.Join(s.deliverableDir, m.Instance+"-"+now.UTC().Format("20060102T150405Z"))
	if _, err := os.Stat(dest); err == nil {
		return "", deliverableManifest{}, errDeliverableExists
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", deliverableManifest{}, err
	}
	cleanup = false
	return dest, m, nil
}

// hashFile returns the SHA-256 hex digest and size of a file.
func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// deliverableHandler serves POST /api/instance/deliverable: the headless
// trigger an orchestrator uses to collect the finished run's bundle. The
// precondition sentinel errors map to 409 (retry later / configure first);
// anything else is a 500. Success returns the bundle directory's basename
// only — the host path stays server-side — plus the manifest.
func (s *Server) deliverableHandler(w http.ResponseWriter, r *http.Request) {
	dir, m, err := s.writeDeliverable(r.Context(), time.Now())
	if err != nil {
		switch {
		case errors.Is(err, errDeliverableNoDir),
			errors.Is(err, errDeliverableNotProvisioned),
			errors.Is(err, errDeliverableNoTask),
			errors.Is(err, errDeliverableNotTerminal),
			errors.Is(err, errDeliverableExists):
			writeErr(w, 409, locale.ErrorMessage(responseLanguage(w), err))
		default:
			writeError(w, 500, err)
		}
		return
	}
	writeJSON(w, 200, map[string]any{"dir": filepath.Base(dir), "manifest": m})
}
