import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const taskInputSource = readFileSync(
  new URL("../src/components/console/task/task-input.tsx", import.meta.url),
  "utf8",
);
const defaultTaskDialogSource = readFileSync(
  new URL("../src/components/console/task/create-default-task-dialog.tsx", import.meta.url),
  "utf8",
);

test("任务输入框在 offline 模式下也加载并展示 Skills", () => {
  assert.match(taskInputSource, /apiRequest\('v1SkillsList'/);
  assert.doesNotMatch(taskInputSource, /const fetchSkillList = \(\) => \{\s*if \(IS_OFFLINE_EDITION\)/);
  assert.doesNotMatch(taskInputSource, /\{!IS_OFFLINE_EDITION && \(\s*<TaskSkillSelector/);
  assert.match(taskInputSource, /filterSelectableSkillIds\(prev, skills\)/);
});

test("默认任务弹窗在 offline 模式下也加载并展示 Skills", () => {
  // Le dialogue charge les Skills via apiRequest dans un useEffect (dépendances
  // actuelles : open, skillList, skillsLoaded, t) — sans garde offline.
  assert.match(defaultTaskDialogSource, /useEffect\(\(\) => \{/);
  assert.match(defaultTaskDialogSource, /apiRequest\("v1SkillsList"/);
  assert.match(defaultTaskDialogSource, /\[open, skillList, skillsLoaded, t\]\)/);
  assert.doesNotMatch(defaultTaskDialogSource, /if \(IS_OFFLINE_EDITION\)/);
  assert.doesNotMatch(defaultTaskDialogSource, /\{!IS_OFFLINE_EDITION && \(\s*<TaskSkillSelector/);
  assert.match(defaultTaskDialogSource, /filterSelectableSkillIds\(prev, skills\)/);
  assert.match(defaultTaskDialogSource, /filterSelectableSkillIds\(defaultSkills, skillList\)/);
});
