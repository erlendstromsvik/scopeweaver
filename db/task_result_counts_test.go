package db

import (
	"fmt"
	"testing"
	"time"
)

// TestTaskResultCounts covers the D.3 counts helper: findings keyed by task_id,
// assets matched through the task_ids array. DB-backed: skips without a test
// database (live check: controller).
func TestTaskResultCounts(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()

	task, err := d.CreateTask("counts task", "counts goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteTask(task.ID) })

	for i := range 2 {
		if _, err := d.AddFinding(task.ID, 0, "TEST", fmt.Sprintf("f%d", i), "low", "s", "e", "w", nil); err != nil {
			t.Fatal(err)
		}
	}
	// One asset tagged with the task.
	if _, err := d.Assets().UpsertRootDomain(UpsertRootDomainReq{
		Domain: fmt.Sprintf("counts-%d.example.test", time.Now().UnixNano()),
		TaskID: task.ID,
	}); err != nil {
		t.Fatal(err)
	}

	findings, assets, err := d.TaskResultCounts(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if findings != 2 || assets != 1 {
		t.Errorf("counts: got (%d,%d), want (2,1)", findings, assets)
	}

	// Unknown task: zero counts, no error.
	findings, assets, err = d.TaskResultCounts(task.ID + 999999)
	if err != nil {
		t.Fatal(err)
	}
	if findings != 0 || assets != 0 {
		t.Errorf("unknown task counts: got (%d,%d), want (0,0)", findings, assets)
	}
}
