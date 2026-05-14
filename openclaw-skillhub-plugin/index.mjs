import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const pluginDir = dirname(fileURLToPath(import.meta.url));
const guidancePath = resolve(pluginDir, "guidance.md");

export function extractGuidanceBlock(markdown) {
  const match = markdown.match(/```md\n([\s\S]*?)\n```/);
  if (!match) {
    throw new Error("SkillHub guidance markdown code block not found");
  }
  return match[1];
}

export function loadGuidance() {
  return readFileSync(guidancePath, "utf8").trim();
}

const BLOCK_REASON =
  "SkillHub skill instructions must be loaded through `load`. Use the sub-skill id from `sub_skills`.";
const DEFAULT_INJECTION_BUDGET_CHARS = 4096;
const MAX_COMPACTION_AGE = 3;

function normalizePathLike(value) {
  return String(value).replaceAll("\\", "/");
}

function isSkillHubInstructionPath(value) {
  const normalized = normalizePathLike(value);
  if (!normalized.includes("SKILL.md")) {
    return false;
  }
  return (
    normalized.includes("/tmp/.llar/") ||
    normalized.startsWith("~/.skillhub/skills/") ||
    normalized.includes("/.skillhub/skills/") ||
    normalized.includes("$SKILLHUB_HOME/skills/")
  );
}

function collectStrings(value, out = []) {
  if (typeof value === "string") {
    out.push(value);
    return out;
  }
  if (Array.isArray(value)) {
    for (const item of value) {
      collectStrings(item, out);
    }
    return out;
  }
  if (value && typeof value === "object") {
    for (const item of Object.values(value)) {
      collectStrings(item, out);
    }
  }
  return out;
}

export function shouldBlockSkillHubInstructionRead(event) {
  const toolName = String(event?.toolName ?? "");
  const params = event?.params ?? {};
  const values = collectStrings(params);
  if (values.some(isSkillHubInstructionPath)) {
    return true;
  }

  if (toolName !== "exec" && toolName !== "shell") {
    return false;
  }
  return values.some((value) => {
    const normalized = normalizePathLike(value);
    return (
      normalized.includes("SKILL.md") &&
      (normalized.includes("/tmp/.llar") ||
        normalized.includes(".skillhub/skills") ||
        normalized.includes("$SKILLHUB_HOME/skills"))
    );
  });
}

function sessionKeyFrom(event, ctx) {
  return (
    ctx?.sessionKey ??
    event?.sessionKey ??
    ctx?.sessionId ??
    event?.sessionId ??
    ctx?.runId ??
    event?.runId ??
    "global"
  );
}

function parseMaybeJson(value) {
  if (typeof value !== "string") {
    return value;
  }
  try {
    return JSON.parse(value);
  } catch {
    return undefined;
  }
}

function extractTextBlocks(value, out = []) {
  if (typeof value === "string") {
    out.push(value);
    return out;
  }
  if (Array.isArray(value)) {
    for (const item of value) {
      extractTextBlocks(item, out);
    }
    return out;
  }
  if (value && typeof value === "object") {
    if (typeof value.text === "string") {
      out.push(value.text);
    }
    if (typeof value.content === "string" || Array.isArray(value.content)) {
      extractTextBlocks(value.content, out);
    }
  }
  return out;
}

function extractLoadedSkill(result) {
  if (!result) {
    return undefined;
  }
  if (typeof result === "object" && !Array.isArray(result) && typeof result.id === "string") {
    return result;
  }
  for (const text of extractTextBlocks(result)) {
    const parsed = parseMaybeJson(text);
    if (parsed && typeof parsed === "object" && typeof parsed.id === "string") {
      return parsed;
    }
  }
  return undefined;
}

function isSkillHubLoad(event) {
  const toolName = String(event?.toolName ?? "");
  return toolName === "load" || toolName.endsWith("__load") || toolName.endsWith(".load");
}

function summarizeSkill(skill) {
  return String(skill.description ?? skill.summary ?? "").replace(/\s+/g, " ").trim();
}

function resolveInjectionBudget(api) {
  const contextWindow = Number(api?.pluginConfig?.contextWindow ?? api?.pluginConfig?.maxContextWindow);
  if (Number.isFinite(contextWindow) && contextWindow > 0) {
    return Math.max(Math.floor(contextWindow * 0.05), DEFAULT_INJECTION_BUDGET_CHARS);
  }
  return DEFAULT_INJECTION_BUDGET_CHARS;
}

function trimToBudget(value, budget) {
  if (value.length <= budget) {
    return value;
  }
  return value.slice(0, Math.max(0, budget - 3)).trimEnd() + "...";
}

function formatLoadedSkills(state, budget) {
  const candidates = Array.from(state.loadedById.values())
    .filter((entry) => state.compactionSeq - entry.loadedCompactionSeq <= MAX_COMPACTION_AGE)
    .sort((a, b) => b.loadedAt - a.loadedAt);
  if (candidates.length === 0) {
    return undefined;
  }

  const lines = [
    "SkillHub skills loaded in this session:",
  ];
  for (const entry of candidates) {
    const name = entry.name ? ` (${entry.name})` : "";
    const summary = entry.summary ? `: ${entry.summary}` : "";
    const line = `- ${entry.id}${name}${summary}`;
    const next = [...lines, line, "", "If relevant, continue using these loaded skills. Search SkillHub if none fits."].join("\n");
    if (next.length > budget) {
      if (lines.length === 1) {
        lines.push(trimToBudget(line, Math.max(0, budget - lines[0].length - 2)));
      }
      break;
    }
    lines.push(line);
  }
  lines.push("", "If relevant, continue using these loaded skills. Search SkillHub if none fits.");
  return trimToBudget(lines.join("\n"), budget);
}

function createLoadedSkillStore() {
  const sessions = new Map();

  function getState(key) {
    let state = sessions.get(key);
    if (!state) {
      state = { compactionSeq: 0, loadedById: new Map() };
      sessions.set(key, state);
    }
    return state;
  }

  return {
    recordLoad(event, ctx) {
      if (event?.error || !isSkillHubLoad(event)) {
        return;
      }
      const skill = extractLoadedSkill(event.result);
      if (!skill?.id) {
        return;
      }
      const key = sessionKeyFrom(event, ctx);
      const state = getState(key);
      state.loadedById.set(skill.id, {
        id: skill.id,
        name: String(skill.name ?? ""),
        summary: summarizeSkill(skill),
        version: String(skill.version ?? ""),
        loadedAt: Date.now(),
        loadedCompactionSeq: state.compactionSeq,
      });
    },
    incrementCompaction(event, ctx) {
      const key = sessionKeyFrom(event, ctx);
      getState(key).compactionSeq += 1;
    },
    formatForPrompt(event, ctx, budget) {
      const key = sessionKeyFrom(event, ctx);
      const state = sessions.get(key);
      if (!state) {
        return undefined;
      }
      return formatLoadedSkills(state, budget);
    },
    clear(event, ctx) {
      sessions.delete(sessionKeyFrom(event, ctx));
      if (event?.nextSessionKey || event?.nextSessionId) {
        sessions.delete(event.nextSessionKey ?? event.nextSessionId);
      }
    },
  };
}

const plugin = {
  id: "skillhub",
  name: "SkillHub",
  description: "Injects canonical SkillHub agent guidance into OpenClaw prompt builds.",
  register(api) {
    const loadedSkills = createLoadedSkillStore();

    api.on("before_prompt_build", async (event, ctx) => {
      const loadedContext = loadedSkills.formatForPrompt(
        event,
        ctx,
        resolveInjectionBudget(api),
      );
      return {
        prependSystemContext: loadGuidance(),
        ...(loadedContext ? { prependContext: loadedContext } : {}),
      };
    });
    api.on("before_tool_call", async (event) => {
      if (!shouldBlockSkillHubInstructionRead(event)) {
        return;
      }
      return {
        block: true,
        blockReason: BLOCK_REASON,
      };
    });
    api.on("after_tool_call", async (event, ctx) => {
      loadedSkills.recordLoad(event, ctx);
    });
    api.on("after_compaction", async (event, ctx) => {
      loadedSkills.incrementCompaction(event, ctx);
    });
    api.on("session_end", async (event, ctx) => {
      loadedSkills.clear(event, ctx);
    });
    api.on("before_reset", async (event, ctx) => {
      loadedSkills.clear(event, ctx);
    });
  },
};

export default plugin;
