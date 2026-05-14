# SkillHub

SkillHub is a skill registry and management system for AI agents. It provides skill discovery (search), installation, dependency resolution (MVS), security scanning, and a standard MCP interface for agent integration.

## Architecture

```
Agent → MCP (skillhub_search / skillhub_load) → SkillHubCore → Discovery Center / Installer
```

- **`skillhub_search`** queries the Discovery Center (PostgreSQL) for approved skills
- **`skillhub_load`** installs (if needed) and loads a skill from the local filesystem
- Discovery Center is the sole search entry point — no direct filesystem search

## Modules

| Package | Responsibility | Key Types |
|---------|---------------|-----------|
| `pkg/types` | Core data types and interfaces | `Skill`, `SkillSummary`, `SearchRequest`, `LoadRequest`, `SkillHubTools` |
| `pkg/parser` | SKILL.md YAML frontmatter parsing | `ParseRoot`, `ParseSubSkill`, `ParseResult` |
| `pkg/vcs` | Git operations (clone, tag resolution) | `Clone`, `ResolveVersion`, `RepoURL`, `SelectLatestVersion` |
| `pkg/resolver` | MVS dependency solver | `Resolver`, `DepFetcher` |
| `pkg/installer` | Local installation management | `Installer`, `Ensure`, `Install` |
| `pkg/loader` | SKILL.md file loading | `LoadRoot`, `LoadSub` |
| `pkg/discovery` | PostgreSQL-backed discovery center | `Discovery` (Search, Register, Approve) |
| `pkg/security` | Three-layer security scanning | `RuleScanner`, `VirusTotalScanner`, `ChainScanner` |
| `pkg/mcp` | JSON-RPC 2.0 stdio MCP server | `Server`, wraps `SkillHubTools` |
| `cmd/skillhub` | CLI entry point | `SkillHubCore` (wires all modules) |

## How to Build

```bash
go build ./cmd/skillhub/
```

All packages (no DB needed):
```bash
go test ./pkg/... -v
```

Discovery tests require PostgreSQL:
```bash
SKILLHUB_TEST_DATABASE_URL=postgres://localhost:5432/skillhub_test go test ./pkg/discovery/ -v
```

## How to Run

MCP server (stdio, for AI agents):
```bash
SKILLHUB_DATABASE_URL=postgres://localhost:5432/skillhub skillhub serve
```

CLI search:
```bash
SKILLHUB_DATABASE_URL=postgres://localhost:5432/skillhub skillhub search "xiaohongshu"
```

CLI load:
```bash
SKILLHUB_DATABASE_URL=postgres://localhost:5432/skillhub skillhub load github.com/acme/clawhub/social/publish-post
```

## Environment Variables

| Variable | Required | Default | Purpose |
|----------|----------|---------|---------|
| `SKILLHUB_DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `SKILLHUB_EMBED_URL` | Yes for semantic discovery | — | Embedding API endpoint, e.g. `http://127.0.0.1:8397/v1/embed` |
| `SKILLHUB_HOME` | No | `$HOME/.skillhub` | Local skill installation root |
| `VT_API_KEY` | No | — | VirusTotal API key (optional scanning) |

## Key Design Decisions

- **MVS dependency resolution**: BFS-based, keeps highest version for duplicate IDs. Subtree-level (not fully global) due to lazy depFetcher.
- **Security scanning**: Three-layer (rules → VirusTotal → AI review). Only RuleScanner is always active; VT requires API key.
- **Discovery center**: PostgreSQL + pgvector semantic search for description/tag intent; ID search still uses exact/prefix matching. Local cache must not short-circuit discovery search.
- **SKILL.md format**: YAML frontmatter (`---` delimited) with `id`, `name`, `description`, `tags`, `dependencies.{tools,skills}`, `skills` fields.
- **Local layout**: `$SKILLHUB_HOME/skills/{host}/{owner}/{repo}/.../{rest}/{version}/`
- **MCP transport**: JSON-RPC 2.0 over stdio, compatible with Claude Code and other MCP hosts.

## Testing Approach

- Packages without external dependencies use standard `go test`
- Git-dependent tests create local git repos (skip if no `git` on PATH)
- PostgreSQL-dependent tests skip if `SKILLHUB_TEST_DATABASE_URL` not set
- Following existing test patterns (external test packages, table-driven tests)
