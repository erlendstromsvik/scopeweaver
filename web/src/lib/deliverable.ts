import type { InstanceStatus } from "./types";

// deliverableButtonState decides whether the "Export deliverable" button shows on
// a task detail page, and whether it is actionable. The deliverable belongs to
// the blueprint instance's own task: hide the button when the instance is not
// provisioned or its task is a different one; wait while the run is still going;
// ready once the task is terminal.
export function deliverableButtonState(status: InstanceStatus | null, taskId: string): "hidden" | "waiting" | "ready" {
  if (!status?.provisioned || status.task_id !== taskId) return "hidden";
  if (!status.terminal) return "waiting";
  return "ready";
}
