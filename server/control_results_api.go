package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Autumn-27/artex/db"
)

// Control results are read-only here: this is the human/UI entry point for the
// per-requirement verdicts recorded by the data layer (db/control_results.go).
// The agent tool that writes verdicts and requirement-catalog resolution are later
// E.3b subtasks; this endpoint only lists a task's current rows.

// controlResultDTO is the frontend shape of one control_results row.
type controlResultDTO struct {
	ID             int64     `json:"id"`
	TaskID         int64     `json:"task_id"`
	Standard       string    `json:"standard"`
	RequirementID  string    `json:"requirement_id"`
	Verdict        string    `json:"verdict"`
	Method         string    `json:"method"`
	EvidenceRef    string    `json:"evidence_ref"`
	Reason         string    `json:"reason"`
	Actor          string    `json:"actor"`
	AssetsCovered  []int64   `json:"assets_covered"`
	LinkedFindings []int64   `json:"linked_findings"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// controlResultDTOs converts db rows to the frontend shape, normalizing nil id
// lists to empty slices so the JSON is always [] and never null.
func controlResultDTOs(rows []*db.ControlResult) []controlResultDTO {
	out := make([]controlResultDTO, 0, len(rows))
	for _, c := range rows {
		assets := c.AssetsCovered
		if assets == nil {
			assets = []int64{}
		}
		linked := c.LinkedFindings
		if linked == nil {
			linked = []int64{}
		}
		out = append(out, controlResultDTO{
			ID:             c.ID,
			TaskID:         c.TaskID,
			Standard:       c.Standard,
			RequirementID:  c.RequirementID,
			Verdict:        c.Verdict,
			Method:         c.Method,
			EvidenceRef:    c.EvidenceRef,
			Reason:         c.Reason,
			Actor:          c.Actor,
			AssetsCovered:  assets,
			LinkedFindings: linked,
			CreatedAt:      c.CreatedAt,
			UpdatedAt:      c.UpdatedAt,
		})
	}
	return out
}

// listControlResults returns all control results for a task, ordered by standard
// then requirement id.
func (s *Server) listControlResults(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	rows, err := s.m.pg.ListControlResults(taskID)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"control_results": controlResultDTOs(rows)})
}
