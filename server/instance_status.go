package server

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Autumn-27/artex/db"
)

// instanceStatus is the machine-readable surface an orchestrator polls to follow
// a blueprint instance's single task without the UI (task D.3).
type instanceStatus struct {
	Provisioned bool   `json:"provisioned"`
	Blueprint   string `json:"blueprint,omitempty"`
	TaskID      string `json:"task_id,omitempty"`
	Status      string `json:"status,omitempty"`
	Terminal    bool   `json:"terminal"`
	Findings    int    `json:"findings"`
	Assets      int    `json:"assets"`
}

// instanceStatusNow reads the current blueprint instance state from settings and
// the task registry. Unprovisioned instances report {provisioned:false}; a task
// row that vanished (deleted) reports status "missing" as terminal, so a poller
// never waits forever on a task that will never complete.
func (s *Server) instanceStatusNow() (instanceStatus, error) {
	pg := s.m.PG()
	name, ok, err := pg.GetSetting(blueprintProvisionedKey)
	if err != nil {
		return instanceStatus{}, err
	}
	if !ok {
		return instanceStatus{Provisioned: false}, nil
	}
	st := instanceStatus{Provisioned: true, Blueprint: name}
	rawID, ok, err := pg.GetSetting(blueprintTaskIDKey)
	if err != nil {
		return st, err
	}
	if !ok {
		return st, nil
	}
	st.TaskID = rawID
	taskID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		return st, err
	}
	task, err := pg.GetTask(taskID)
	if err != nil {
		return st, err
	}
	if task == nil {
		st.Status = "missing"
		st.Terminal = true
		return st, nil
	}
	st.Status = task.Status
	st.Terminal = db.IsTerminal(task.Status)
	findings, assets, err := pg.TaskResultCounts(taskID)
	if err != nil {
		return st, err
	}
	st.Findings = findings
	st.Assets = assets
	return st, nil
}

func (s *Server) instanceStatusHandler(w http.ResponseWriter, r *http.Request) {
	st, err := s.instanceStatusNow()
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, st)
}

// blueprintCompleteFile is the sentinel an orchestrator watches on the host mount.
const blueprintCompleteFile = "blueprint-complete.json"

// WatchBlueprintCompletion polls the blueprint task and, once it reaches a
// terminal state, emits the completion marker and stops. It returns immediately
// when this instance never admitted a blueprint task. Errors are logged, not
// fatal: a transient DB hiccup must not cancel the watch.
func (s *Server) WatchBlueprintCompletion(interval time.Duration) {
	pg := s.m.PG()
	if _, ok, err := pg.GetSetting(blueprintTaskIDKey); err != nil || !ok {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			// Check once before the first tick so an already-terminal task is
			// reported at boot.
			if s.reportBlueprintCompletion() {
				return
			}
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// reportBlueprintCompletion logs the marker and writes the sentinel file when
// the task is terminal. Returns whether watching should stop (only on terminal;
// errors keep the watch alive).
func (s *Server) reportBlueprintCompletion() bool {
	st, err := s.instanceStatusNow()
	if err != nil {
		log.Printf("[blueprint] status check: %v", err)
		return false
	}
	if !st.Terminal {
		return false
	}
	// Machine-readable marker: deliberately log.Printf, NOT locale.Text — the
	// orchestrator greps this exact line and must not depend on the UI language.
	log.Printf("[blueprint] COMPLETE task=#%s status=%s findings=%d assets=%d",
		st.TaskID, st.Status, st.Findings, st.Assets)
	if err := writeBlueprintComplete(filepath.Join(s.m.dir, blueprintCompleteFile), st); err != nil {
		log.Printf("[blueprint] write completion file: %v", err)
		return false
	}
	return true
}

// writeBlueprintComplete writes the status JSON atomically: a temp file in the
// same directory, then rename, so a poller never observes a half-written file.
func writeBlueprintComplete(path string, st instanceStatus) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), blueprintCompleteFile+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
