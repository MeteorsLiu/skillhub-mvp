import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";

import plugin, { loadGuidance, shouldBlockSkillHubInstructionRead } from "../index.mjs";

test("loadGuidance returns the packaged guidance", async () => {
  const packaged = (await readFile(new URL("../guidance.md", import.meta.url), "utf8")).trim();

  const guidance = loadGuidance();

  assert.equal(guidance, packaged);
  assert.match(guidance, /SkillHub fills capability gaps/);
  assert.match(guidance, /teach you how to do EVERYTHING/);
  assert.match(guidance, /For every user request, check whether an existing skill could improve the result/);
  assert.match(guidance, /ALWAYS assume a relevant specialized skill may exist/);
  assert.match(guidance, /MUST search SkillHub FIRST/);
  assert.match(guidance, /Do not load, install, or execute a skill until the user confirms/);
});

test("plugin injects canonical guidance through before_prompt_build", async () => {
  const hooks = new Map();
  const api = {
    on(name, handler, options) {
      hooks.set(name, { handler, options });
    },
  };

  plugin.register(api);

  assert.equal(hooks.has("before_prompt_build"), true);
  assert.equal(hooks.has("before_tool_call"), true);
  assert.equal(hooks.has("tool_result_persist"), false);
  const result = await hooks.get("before_prompt_build").handler();
  assert.equal(result.prependSystemContext, loadGuidance());
});

test("plugin registers loaded-skill lifecycle hooks", () => {
  const hooks = new Map();
  const api = {
    on(name, handler, options) {
      hooks.set(name, { handler, options });
    },
  };

  plugin.register(api);

  assert.equal(hooks.has("after_tool_call"), true);
  assert.equal(hooks.has("after_compaction"), true);
  assert.equal(hooks.has("session_end"), true);
  assert.equal(hooks.has("before_reset"), true);
});

test("successful SkillHub load is injected into session prompt context", async () => {
  const hooks = new Map();
  const api = {
    on(name, handler, options) {
      hooks.set(name, { handler, options });
    },
  };
  plugin.register(api);

  await hooks.get("after_tool_call").handler(
    {
      toolName: "skillhub__load",
      params: { id: "github.com/acme/travel-planner" },
      result: {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              id: "github.com/acme/travel-planner",
              name: "travel-planner",
              description: "Plan multi-day trips, hotels, flights, and local itinerary tradeoffs.",
              version: "1.2.3",
              body: "full instructions must not be injected",
            }),
          },
        ],
      },
    },
    { sessionKey: "agent:default:s1" },
  );

  const result = await hooks
    .get("before_prompt_build")
    .handler({ prompt: "plan my trip", messages: [] }, { sessionKey: "agent:default:s1" });

  assert.equal(result.prependSystemContext, loadGuidance());
  assert.match(result.prependContext, /SkillHub skills loaded in this session:/);
  assert.match(result.prependContext, /github\.com\/acme\/travel-planner \(travel-planner\)/);
  assert.match(result.prependContext, /Plan multi-day trips/);
  assert.doesNotMatch(result.prependContext, /full instructions must not be injected/);
});

test("loaded-skill injection dedupes by id and keeps the latest load", async () => {
  const hooks = new Map();
  const api = {
    on(name, handler, options) {
      hooks.set(name, { handler, options });
    },
  };
  plugin.register(api);

  const load = async (description, version) => {
    await hooks.get("after_tool_call").handler(
      {
        toolName: "skillhub__load",
        params: { id: "github.com/acme/demo" },
        result: {
          content: [
            {
              type: "text",
              text: JSON.stringify({
                id: "github.com/acme/demo",
                name: "demo",
                description,
                version,
                body: "body",
              }),
            },
          ],
        },
      },
      { sessionId: "s1" },
    );
  };

  await load("old summary", "1.0.0");
  await load("new summary", "2.0.0");

  const result = await hooks
    .get("before_prompt_build")
    .handler({ prompt: "use demo", messages: [] }, { sessionId: "s1" });

  assert.match(result.prependContext, /new summary/);
  assert.doesNotMatch(result.prependContext, /old summary/);
});

test("loaded-skill injection respects budget and compaction aging", async () => {
  const hooks = new Map();
  const api = {
    pluginConfig: { contextWindow: 1000 },
    on(name, handler, options) {
      hooks.set(name, { handler, options });
    },
  };
  plugin.register(api);

  await hooks.get("after_tool_call").handler(
    {
      toolName: "skillhub__load",
      params: { id: "github.com/acme/long" },
      result: {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              id: "github.com/acme/long",
              name: "long",
              description: "x".repeat(6000),
              version: "1.0.0",
              body: "body",
            }),
          },
        ],
      },
    },
    { sessionId: "s1" },
  );

  let result = await hooks
    .get("before_prompt_build")
    .handler({ prompt: "use long", messages: [] }, { sessionId: "s1" });
  assert.ok(result.prependContext.length <= 4096);
  assert.ok(result.prependContext.length > 0);

  for (let i = 0; i < 4; i++) {
    await hooks.get("after_compaction").handler({}, { sessionId: "s1" });
  }

  result = await hooks
    .get("before_prompt_build")
    .handler({ prompt: "use long", messages: [] }, { sessionId: "s1" });
  assert.equal(result.prependContext, undefined);
});

test("session reset and session end clear loaded-skill injection", async () => {
  const hooks = new Map();
  const api = {
    on(name, handler, options) {
      hooks.set(name, { handler, options });
    },
  };
  plugin.register(api);

  const load = async (sessionId) => {
    await hooks.get("after_tool_call").handler(
      {
        toolName: "skillhub__load",
        params: { id: `github.com/acme/${sessionId}` },
        result: {
          content: [
            {
              type: "text",
              text: JSON.stringify({
                id: `github.com/acme/${sessionId}`,
                name: sessionId,
                description: "summary",
                version: "1.0.0",
                body: "body",
              }),
            },
          ],
        },
      },
      { sessionId },
    );
  };

  await load("reset-session");
  await hooks.get("before_reset").handler({}, { sessionId: "reset-session" });
  let result = await hooks
    .get("before_prompt_build")
    .handler({ prompt: "again", messages: [] }, { sessionId: "reset-session" });
  assert.equal(result.prependContext, undefined);

  await load("ended-session");
  await hooks.get("session_end").handler({ sessionId: "ended-session", messageCount: 1 }, {});
  result = await hooks
    .get("before_prompt_build")
    .handler({ prompt: "again", messages: [] }, { sessionId: "ended-session" });
  assert.equal(result.prependContext, undefined);
});

test("plugin blocks SkillHub-managed SKILL.md reads", async () => {
  assert.equal(
    shouldBlockSkillHubInstructionRead({
      toolName: "read",
      params: { path: "/tmp/.llar/github.com/acme/demo@v1.0.0/skills/sub/SKILL.md" },
    }),
    true,
  );
  assert.equal(
    shouldBlockSkillHubInstructionRead({
      toolName: "read",
      params: { path: "~/.skillhub/skills/github.com/acme/demo/v1.0.0/SKILL.md" },
    }),
    true,
  );
  assert.equal(
    shouldBlockSkillHubInstructionRead({
      toolName: "exec",
      params: { command: "cat /tmp/.llar/github.com/acme/demo@v1.0.0/SKILL.md" },
    }),
    true,
  );
});

test("plugin allows ordinary skill files and SkillHub resources", async () => {
  assert.equal(
    shouldBlockSkillHubInstructionRead({
      toolName: "read",
      params: { path: "/home/openclaw/.openclaw/skills/example/SKILL.md" },
    }),
    false,
  );
  assert.equal(
    shouldBlockSkillHubInstructionRead({
      toolName: "read",
      params: { path: "/tmp/.llar/github.com/acme/demo@v1.0.0/references/guide.md" },
    }),
    false,
  );
});

test("before_tool_call returns a block result for SkillHub instructions", async () => {
  const hooks = new Map();
  const api = {
    on(name, handler, options) {
      hooks.set(name, { handler, options });
    },
  };

  plugin.register(api);

  const result = await hooks.get("before_tool_call").handler({
    toolName: "read",
    params: { path: "/tmp/.llar/github.com/acme/demo@v1.0.0/SKILL.md" },
  });
  assert.equal(result.block, true);
  assert.match(result.blockReason, /SkillHub skill instructions/);
});
