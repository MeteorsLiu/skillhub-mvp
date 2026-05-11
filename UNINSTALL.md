# Uninstall SkillHub

This guide removes SkillHub from an agent host cleanly.

SkillHub installation has two parts:

1. The `skillhub` MCP server entry.
2. The SkillHub agent guidance injected into a persistent instruction file.

Remove both, then restart or reload the agent host.

## Remove The Agent Guidance

Find and delete the canonical SkillHub guidance block from the host's persistent instruction file. Use [doc/Skillhub_Agent_Guidance.md](https://raw.githubusercontent.com/MeteorsLiu/skillhub-mvp/refs/heads/main/doc/Skillhub_Agent_Guidance.md) as the source of truth, and remove that exact block wherever it was installed. Do not assume it is at the top.

Common instruction files:

| Host | Remove guidance from |
| --- | --- |
| OpenCode | `~/.config/opencode/AGENTS.md` or project `AGENTS.md` |
| OpenClaw | Workspace `AGENTS.md` |
| PicoClaw | Configured workspace `AGENTS.md`, default `~/.picoclaw/workspace/AGENTS.md` |
| Codex CLI / Codex App | `$CODEX_HOME/AGENTS.md`, default `~/.codex/AGENTS.md`, or project `AGENTS.md` |
| Claude Code | `~/.claude/CLAUDE.md` or project `CLAUDE.md` |
| Gemini CLI | `GEMINI.md` |
| Cursor | `AGENTS.md` or Cursor rules |
| VS Code Copilot Agent mode | `AGENTS.md` |

If the file only contains the SkillHub block and nothing else, you can remove the file instead of editing it.

## OpenCode

Remove the `skillhub` entry from your OpenCode MCP config:

```json
{
  "mcp": {
    "skillhub": {
      "type": "remote",
      "url": "http://218.11.5.155/mcp",
      "enabled": true
    }
  }
}
```

Then remove the SkillHub block from `~/.config/opencode/AGENTS.md` or the project `AGENTS.md`.

Restart OpenCode.

## OpenClaw

Remove the `skillhub` server from the OpenClaw MCP config:

```json
{
  "mcp": {
    "servers": {
      "skillhub": {
        "url": "http://218.11.5.155/mcp",
        "transport": "streamable-http"
      }
    }
  }
}
```

Then remove the SkillHub block from the workspace `AGENTS.md` used by that OpenClaw profile.

Restart the OpenClaw gateway or agent process using the restart method for your deployment.

## PicoClaw

Remove the `skillhub` server from `~/.picoclaw/config.json`:

```json
{
  "tools": {
    "mcp": {
      "servers": {
        "skillhub": {
          "enabled": true,
          "type": "http",
          "url": "http://218.11.5.155/mcp",
          "deferred": false
        }
      }
    }
  }
}
```

Then remove the SkillHub block from the configured workspace `AGENTS.md`.

Restart the PicoClaw gateway or agent process.

## Codex

Remove the MCP server:

```bash
codex mcp remove skillhub
```

If the server was added manually, remove this table from `~/.codex/config.toml`:

```toml
[mcp_servers.skillhub]
url = "http://218.11.5.155/mcp"
```

Then remove the SkillHub block from the global or project `AGENTS.md`.

Restart Codex.

## Claude Code

Remove the MCP server:

```bash
claude mcp remove skillhub
```

Then remove the SkillHub block from `~/.claude/CLAUDE.md` or the project `CLAUDE.md`.

Restart Claude Code, or use `/memory` to verify the updated memory file is loaded.

## Gemini CLI

Remove the `skillhub` server from `~/.gemini/settings.json`:

```json
{
  "mcpServers": {
    "skillhub": {
      "httpUrl": "http://218.11.5.155/mcp"
    }
  }
}
```

Then remove the SkillHub block from `GEMINI.md`.

Restart Gemini CLI.

## Cursor

Remove the `skillhub` server from `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "skillhub": {
      "url": "http://218.11.5.155/mcp"
    }
  }
}
```

Then remove the SkillHub block from `AGENTS.md` or Cursor rules.

Restart Cursor.

## VS Code Copilot Agent Mode

Remove the `skillhub` server from `.vscode/mcp.json`:

```json
{
  "servers": {
    "skillhub": {
      "type": "http",
      "url": "http://218.11.5.155/mcp"
    }
  }
}
```

Then remove the SkillHub block from `AGENTS.md`.

Reload the VS Code window.

## Local Stdio Mode

If you installed a local SkillHub binary, remove the local MCP server entry from the host config first.

Then remove local runtime files if they are not used by anything else:

```bash
rm -rf "$HOME/.skillhub"
```

If you started local Discovery, PostgreSQL, Redis, or SkillHub services only for SkillHub, stop those services using the service manager or container runtime you used to start them.

## Verify Removal

After restarting the host, start a new session and check that these tools are no longer available:

```text
skillhub__search
skillhub__load
```

Also verify the persistent instruction file no longer contains:

```text
SkillHub
skillhub__search
skillhub__load
```

If the tools still appear, the MCP server entry is still present or the host has not reloaded its MCP configuration.

If the agent still follows SkillHub behavior, the guidance block is still present in one of the host's loaded instruction files.
