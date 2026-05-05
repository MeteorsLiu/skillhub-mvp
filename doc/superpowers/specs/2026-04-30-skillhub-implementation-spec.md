# SkillHub MVP Implementation Spec

Based on: `doc/Skillhub_Design_Document.md`
Date: 2026-04-30
Status: Brainstormed and approved

## Decisions Made During Brainstorming

### Architecture

```
Agent → MCP (skillhub_search / skillhub_load) → SkillHub Core → Discovery Center / Installer
```

- `skillhub_search` queries Discovery Center (PostgreSQL registry), not the filesystem
- `skillhub_load` checks local installation directory, triggers remote install if missing
- No independent "searcher" module — search is a Discovery Center responsibility

### PostgreSQL for Discovery Center

- Postgres with `pgx/v5` driver for the skill registry
- Initial search uses `~*` (case-insensitive regex) for description/tag matching
- No pg_trgm/pgvector for now — added later when performance requires it
- Schema: `id TEXT PK, name TEXT, description TEXT, version TEXT, tags TEXT[], approved BOOL, source TEXT, timestamps`

### Three-Layer Security Scanning

1. **Rule Engine** (no external dep) — pattern matching for dangerous patterns: hardcoded tokens, curl/wget to IPs, base64 decode + exec, file overwrite to system paths, reverse shells
2. **VirusTotal API v3** — REST client, scan remote file URLs referenced by skills (graceful skip if no API key)
3. **AI Review** (optional, LLM API) — semantic review of SKILL.md content

Skill is marked `approved: true` only after all configured scanners pass.

## Module Design

### `pkg/types` — Core Data Types
Direct mapping of Section 4 types. No external deps.

### `pkg/parser` — SKILL.md Parser
- Parse YAML frontmatter (`---` delimited) from markdown
- Extract `id`, `name`, `description`, `tags`, `dependencies.{tools,skills}`, `skills`
- Derive default `id` for single-root repos: `github.com/{owner}/{repo}`
- Derive sub-skill `id` from parent `id` + subdirectory path
- Auto-discover `skills/` subdirectory contents when `skills` field is absent

### `pkg/vcs` — Git Operations
- `git clone --depth 1 --branch <tag>` for remote fetch
- List remote tags, parse Go-style semver
- Monorepo support: `social/publish-post/v1.2.3` tag format
- Pseudo-version fallback when no matching tag

### `pkg/resolver` — MVS Dependency Solver
- Parse `dependencies.skills` entries (`id@version`)
- Build dependency graph, recursive resolution
- For duplicate requirements, keep highest version
- Return resolved `map[id]version`

### `pkg/installer` — Local Installation Management
- Directory layout: `$SKILLHUB_HOME/skills/{id-path}/{version}/`
- Install unit: root skill `id@exact_version`
- Flow: resolve version → git clone → place in version dir → install deps recursively
- `Ensure(id, version)`: check local, install if missing

### `pkg/loader` — SKILL.md File Loader
- Read root/sub SKILL.md from installed directory
- Return full `Skill` with body, sub_skills summaries, deps

### `pkg/discovery` — Discovery Center (PostgreSQL)
- `Search(req)`: Postgres query, regex matching on description/tag, prefix matching on id
- `Register(skill)`: upsert into registry
- `Approve(id)`: set `approved = true`
- Auto-create tables on startup

### `pkg/security` — Security Scanning
- `Scanner` interface: `Scan(path) → {Passed, Issues}`
- `RuleScanner`: Go pattern matching, no external dep
- `VirusTotalScanner`: HTTP client for VT API v3
- `ChainScanner`: composite scanner running multiple scanners

### `pkg/mcp` — MCP Server
- JSON-RPC 2.0 over stdio
- Tools: `skillhub_search`, `skillhub_load`
- Compatible with Claude Code, Copilot CLI, and other MCP hosts

### `cmd/skillhub` — CLI
- `skillhub serve` — start stdio MCP server
- `skillhub search <query>` — CLI search shortcut
- `skillhub load <id>` — CLI load shortcut

## File Layout

```
skillhub/
├── cmd/skillhub/main.go
├── pkg/
│   ├── types/types.go
│   ├── parser/parser.go
│   ├── vcs/git.go
│   ├── resolver/resolver.go
│   ├── installer/installer.go
│   ├── loader/loader.go
│   ├── discovery/discovery.go
│   ├── security/scanner.go
│   └── mcp/server.go
├── AGENTS.md
├── doc/
│   ├── Skillhub_Design_Document.md
│   └── superpowers/specs/2026-04-30-skillhub-implementation-spec.md
└── go.mod
```

## Implementation Order

1. `types` → `parser` (no deps beyond yaml)
2. `vcs` → `resolver` (git + MVS)
3. `installer` → `loader` (local management)
4. `discovery` → `security` (services, depends on pgx)
5. `mcp` → `cmd/skillhub` (integration)
6. `AGENTS.md`

Each package has tests.
