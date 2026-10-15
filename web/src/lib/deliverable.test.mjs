import { deliverableButtonState } from "./deliverable.ts";
import assert from "node:assert/strict";
import test from "node:test";

const base = {
  provisioned: true,
  blueprint: "bp.yaml",
  task_id: "42",
  status: "done",
  terminal: true,
  findings: 3,
  assets: 9,
};

test("hidden when there is no status yet", () => {
  assert.equal(deliverableButtonState(null, "42"), "hidden");
});

test("hidden when the instance is not provisioned", () => {
  assert.equal(deliverableButtonState({ ...base, provisioned: false }, "42"), "hidden");
});

test("hidden when the instance's task is a different task", () => {
  assert.equal(deliverableButtonState({ ...base, task_id: "7" }, "42"), "hidden");
  assert.equal(deliverableButtonState({ ...base, task_id: undefined }, "42"), "hidden");
});

test("waiting while the run has not finished", () => {
  assert.equal(deliverableButtonState({ ...base, terminal: false, status: "running" }, "42"), "waiting");
});

test("ready when provisioned, same task, terminal", () => {
  assert.equal(deliverableButtonState(base, "42"), "ready");
  assert.equal(deliverableButtonState({ ...base, status: "failed", terminal: true }, "42"), "ready");
});
