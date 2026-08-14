import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const handlerSource = readFileSync(
  new URL("../src/components/console/task/task-message-handler.ts", import.meta.url),
  "utf8",
);
const errorSource = readFileSync(
  new URL("../src/components/console/task/message-error.tsx", import.meta.url),
  "utf8",
);

test("task errors normalize legacy error.message into visible details", () => {
  assert.match(handlerSource, /data\?\.details/);
  assert.match(handlerSource, /data\?\.error\?\.message/);
  assert.match(handlerSource, /details,/);
  assert.match(errorSource, /message\.data\.details/);
});
