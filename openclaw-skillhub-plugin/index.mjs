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

const plugin = {
  id: "skillhub",
  name: "SkillHub",
  description: "Injects canonical SkillHub agent guidance into OpenClaw prompt builds.",
  register(api) {
    api.on("before_prompt_build", async () => {
      return {
        prependSystemContext: loadGuidance(),
      };
    });
  },
};

export default plugin;
