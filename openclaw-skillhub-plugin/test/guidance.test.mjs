import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";

import plugin, { loadGuidance } from "../index.mjs";

test("loadGuidance returns the packaged guidance", async () => {
  const packaged = (await readFile(new URL("../guidance.md", import.meta.url), "utf8")).trim();

  const guidance = loadGuidance();

  assert.equal(guidance, packaged);
  assert.match(guidance, /Workflow:/);
  assert.match(guidance, /teach you how to do EVERYTHING/);
  assert.match(guidance, /Would a specialized skill make this task easier, safer, more complete, or more accurate/);
  assert.match(guidance, /Web search finds information\. SkillHub teaches you how to do the task/);
});

test("plugin injects canonical guidance through before_prompt_build", async () => {
  let hook;
  const api = {
    on(name, handler) {
      hook = { name, handler };
    },
  };

  plugin.register(api);

  assert.equal(hook.name, "before_prompt_build");
  const result = await hook.handler();
  assert.equal(result.prependSystemContext, loadGuidance());
});
