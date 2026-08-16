import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const panelSource = readFileSync(
  new URL("../src/components/console/task/task-terminal-panel.tsx", import.meta.url),
  "utf8",
);
const detailSource = readFileSync(
  new URL("../src/pages/console/user/task/task-detail.tsx", import.meta.url),
  "utf8",
);
const terminalSource = readFileSync(
  new URL("../src/components/common/terminal.tsx", import.meta.url),
  "utf8",
);

test("local task terminal auto-creates its first session", () => {
  assert.match(panelSource, /existingSessions\.length > 0/);
  assert.match(panelSource, /autoSessionCreatedRef/);
  assert.match(panelSource, /setCurrentSessionId\(newId\)/);
});

test("local persisted workspace keeps terminal available after a task error", () => {
  assert.match(detailSource, /terminalAvailable = taskInteractive \|\| \(IS_OFFLINE_EDITION && Boolean\(envid\)\)/);
  assert.match(detailSource, /disabled=\{!terminalAvailable\}/);
});

test("terminal falls back when WebGL initialization fails", () => {
  assert.match(terminalSource, /Terminal WebGL unavailable, using canvas renderer/);
  assert.match(terminalSource, /catch \(error\)/);
});
