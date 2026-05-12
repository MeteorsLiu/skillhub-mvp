import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";

import plugin, { loadGuidance } from "../index.mjs";

test("loadGuidance returns the packaged guidance", async () => {
  const packaged = (await readFile(new URL("../guidance.md", import.meta.url), "utf8")).trim();

  const guidance = loadGuidance();

  assert.equal(guidance, packaged);
  assert.match(guidance, /Workflow:/);
  assert.match(guidance, /For every user request, ask whether a skill could help/);
  assert.match(guidance, /If multiple skills are needed, load the best skill for each sub-task/);
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
