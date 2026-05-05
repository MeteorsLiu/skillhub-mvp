# SkillHub MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Build full SkillHub MVP — skill registry, search, load, install, dependency resolution (MVS), VCS integration, security scanning, MCP server.

**Architecture:** Go library + stdio MCP server. PostgreSQL discovery center. Git-based remote install. Three-layer security scanning.

**Tech Stack:** Go 1.24, `gopkg.in/yaml.v3`, `github.com/jackc/pgx/v5`

---

### Task 1: Core Data Types

**Files:** `pkg/types/types.go`, `pkg/types/types_test.go`

- [ ] **Write tests for ParseDependency, Validate, RootID**

```go
// pkg/types/types_test.go
package types_test

import (
	"testing"
	"skillhub/pkg/types"
)

func TestParseDependency(t *testing.T) {
	id, ver, err := types.ParseDependency("github.com/acme/A@v1.2.0")
	if id != "github.com/acme/A" { t.Fatal("bad id") }
	if ver != "v1.2.0" { t.Fatal("bad version") }
	if err != nil { t.Fatal(err) }
}

func TestParseDependency_MissingVersion(t *testing.T) {
	_, _, err := types.ParseDependency("github.com/acme/A")
	if err == nil { t.Fatal("expected error") }
}

func TestParseDependency_MultipleAt(t *testing.T) {
	_, _, err := types.ParseDependency("a@b@c")
	if err == nil { t.Fatal("expected error") }
}

func TestSearchRequestValidate_Empty(t *testing.T) {
	if err := types.SearchRequest{}.Validate(); err == nil { t.Fatal("expected error") }
}

func TestSearchRequestValidate_WithID(t *testing.T) {
	if err := types.SearchRequest{ID: "test"}.Validate(); err != nil { t.Fatal(err) }
}

func TestLoadRequestValidate_Empty(t *testing.T) {
	if err := types.LoadRequest{}.Validate(); err == nil { t.Fatal("expected error") }
}

func TestRootID(t *testing.T) {
	got := types.RootID("github.com/acme/clawhub/social/publish-post/draft-post")
	if got != "github.com/acme/clawhub/social/publish-post" { t.Fatal("bad root id") }
}
```

- [ ] **Run test: `go test ./pkg/types/ -v`** → FAIL (no package)
- [ ] **Implement types.go** — `SearchRequest`, `LoadRequest`, `SkillSummary`, `SkillDeps`, `Skill`, `SkillHubTools` interface, `ParseDependency()`, `RootID()`, validate methods per design doc Section 4 types.
- [ ] **Run test** → PASS
- [ ] **Conditional commit: `git add pkg/types/ && git commit -m "feat: add core data types"`**

---

### Task 2: SKILL.md Parser

**Files:** `pkg/parser/parser.go`, `pkg/parser/parser_test.go`
**Test data:**
- `pkg/parser/testdata/root-skill/SKILL.md` — full root skill with id, name, desc, tags, deps, sub-skills
- `pkg/parser/testdata/root-skill/skills/sub-a/SKILL.md` — sub skill, no id
- `pkg/parser/testdata/root-skill/skills/sub-b/SKILL.md` — sub skill, no id
- `pkg/parser/testdata/minimal/SKILL.md` — minimal root skill

- [ ] **Create test fixture files** with correct YAML frontmatter (match design doc Section 3.2/3.3)

`testdata/root-skill/SKILL.md`:
```markdown
---
id: github.com/acme/clawhub/social/publish-post
name: 发布小红书图文
description: 发布小红书图文内容
tags:
  - social
  - xiaohongshu
dependencies:
  tools:
    - ffmpeg
    - yt-dlp
  skills:
    - github.com/acme/clawhub/common/image-tools@v1.2.0
skills:
  - sub-a
  - sub-b
---
# Publish Post

Content body here.
```

`testdata/root-skill/skills/sub-a/SKILL.md`:
```markdown
---
name: Sub A
description: Sub skill A
tags:
  - writing
dependencies:
  tools: []
  skills: []
skills: []
---
# Sub A
```

`testdata/minimal/SKILL.md`:
```markdown
---
id: github.com/bob/rednote-skill
name: Rednote Skill
description: Post to rednote
tags: []
dependencies:
  tools: []
  skills: []
skills: []
---
# Minimal
```

- [ ] **Write tests:**

```go
func TestParseRootSkill(t *testing.T) {
    result, err := parser.ParseRoot("testdata/root-skill")
    if err != nil { t.Fatal(err) }
    // check result.ID, result.Name, result.Description, result.Tags, result.Deps.Tools[0]=="ffmpeg", result.SubSkillPaths[0]=="sub-a"
}

func TestParseRoot_MissingID(t *testing.T) {
    _, err := parser.ParseRoot("testdata/root-skill/skills/sub-a")
    if err == nil { t.Fatal("expected error") }
}

func TestParseSubSkill(t *testing.T) {
    result, err := parser.ParseSubSkill("testdata/root-skill/skills/sub-a/SKILL.md", "github.com/acme/clawhub/social/publish-post")
    if err != nil { t.Fatal(err) }
    if result.ID != "github.com/acme/clawhub/social/publish-post/sub-a" { t.Fatal("bad sub id") }
}

func TestDiscoverSubSkills(t *testing.T) {
    names, err := parser.DiscoverSubSkills("testdata/root-skill")
    if err != nil { t.Fatal(err) }
    if len(names) != 2 { t.Fatal("expected 2") }
}
```

- [ ] **Run test: `go mod tidy && go test ./pkg/parser/ -v`** → FAIL
- [ ] **Implement parser.go** with `ParseRoot(dir)`, `ParseSubSkill(path, parentID)`, `ParseFrontmatter(raw)`, `DiscoverSubSkills(dir)`. Parse YAML frontmatter with `gopkg.in/yaml.v3`. Split `---` delimiters. Handle missing `skills` field by auto-discovering `skills/` dir. Return `ParseResult` struct with `{ID, Name, Description, Tags, Deps SkillDeps, Body, SubSkillPaths []string}`.
- [ ] **Run test** → PASS
- [ ] **Conditional commit**

---

### Task 3: Git VCS Operations

**Files:** `pkg/vcs/git.go`, `pkg/vcs/git_test.go`

- [ ] **Write tests:**

```go
func TestParseTagVersion(t *testing.T) {
    v := vcs.ParseTagVersion("v1.2.3")
    if v != "v1.2.3" { t.Fatal("bad parse") }
}

func TestParseTagVersion_Monorepo(t *testing.T) {
    v := vcs.ParseTagVersion("social/publish-post/v1.2.3")
    if v != "v1.2.3" { t.Fatal("expected v1.2.3, got", v) }
}

func TestParseTagVersion_NonVersion(t *testing.T) {
    v := vcs.ParseTagVersion("v1.2")
    if v != "" { t.Fatal("expected empty for non-semver") }
}

func TestSelectLatestVersion(t *testing.T) {
    tags := []string{"v1.0.0", "v1.2.0", "v1.1.0", "v2.0.0-alpha"}
    latest := vcs.SelectLatestVersion(tags)
    if latest != "v1.2.0" { t.Fatal("expected v1.2.0, got", latest) }
}

func TestPseudoVersion(t *testing.T) {
    pv := vcs.PseudoVersion("abc1234", "2026-04-30T10:00:00Z")
    if pv == "" { t.Fatal("expected pseudo version") }
    // format: v0.0.0-20260430-abc1234
}

func TestRepoURL(t *testing.T) {
    cases := []struct{id, url string}{
        {"github.com/acme/clawhub/social/publish-post", "https://github.com/acme/clawhub"},
        {"github.com/bob/rednote-skill", "https://github.com/bob/rednote-skill"},
    }
    for _, c := range cases {
        url := vcs.RepoURL(c.id)
        if url != c.url {
            t.Fatalf("for %s: expected %s, got %s", c.id, c.url, url)
        }
    }
}
```

- [ ] **Run test:** `go test ./pkg/vcs/ -v` → FAIL
- [ ] **Implement git.go:**
  - `ParseTagVersion(tag string) string` — extract Go semver from tag (plain or monorepo path)
  - `SelectLatestVersion(tags []string) string` — compare with semver, skip pre-release for stable selection
  - `PseudoVersion(commitHash, commitTime string) string` — generate `v0.0.0-YYYYMMDD-HHMMSS-commithash`
  - `RepoURL(id string) string` — derive git repo URL from skill ID: everything up to `host/owner/repo`
  - `Clone(repoURL, version, targetDir string) error` — run `git clone --depth 1 --branch <tag> <url> <dir>`
  - `ListRemoteTags(repoURL string) ([]string, error)` — run `git ls-remote --tags <url>`, parse tag refs
  - `ResolveVersion(repoURL string, constraint string) (string, error)` — best tag matching constraint, fallback pseudo-version
- [ ] **Run test** → PASS
- [ ] **Conditional commit**

---

### Task 4: MVS Dependency Resolver

**Files:** `pkg/resolver/resolver.go`, `pkg/resolver/resolver_test.go`

- [ ] **Write tests:**

```go
func TestResolveSimple(t *testing.T) {
    deps := []types.SkillSummary{
        {ID: "github.com/acme/A", Version: "v1.2.0"},
    }
    r := resolver.New(nil)
    result, err := r.Resolve(deps)
    if err != nil { t.Fatal(err) }
    if result["github.com/acme/A"] != "v1.2.0" { t.Fatal("bad version") }
}

func TestResolve_MVSKeepsHighest(t *testing.T) {
    deps := []types.SkillSummary{
        {ID: "github.com/acme/A", Version: "v1.2.0"},
        {ID: "github.com/acme/A", Version: "v1.5.0"},
    }
    r := resolver.New(nil)
    result, err := r.Resolve(deps)
    if err != nil { t.Fatal(err) }
    if result["github.com/acme/A"] != "v1.5.0" { t.Fatal("expected v1.5.0") }
}

func TestResolveTransitive(t *testing.T) {
    // A@v1.0.0 depends on C@v1.3.0
    // B@v1.0.0 depends on C@v1.4.0
    // Result: C = v1.4.0 (MVS)
    resolver := resolver.New(func(id string) ([]types.SkillSummary, error) {
        switch id {
        case "github.com/acme/A":
            return []types.SkillSummary{{ID: "github.com/acme/C", Version: "v1.3.0"}}, nil
        case "github.com/acme/B":
            return []types.SkillSummary{{ID: "github.com/acme/C", Version: "v1.4.0"}}, nil
        default:
            return nil, nil
        }
    })
    deps := []types.SkillSummary{
        {ID: "github.com/acme/A", Version: "v1.0.0"},
        {ID: "github.com/acme/B", Version: "v1.0.0"},
    }
    result, err := resolver.Resolve(deps)
    if err != nil { t.Fatal(err) }
    if result["github.com/acme/C"] != "v1.4.0" {
        t.Fatalf("expected C=v1.4.0, got %s", result["github.com/acme/C"])
    }
}
```

- [ ] **Run test:** `go test ./pkg/resolver/ -v` → FAIL
- [ ] **Implement resolver.go:**
  - `type Resolver struct { fetchDeps func(id string) ([]SkillSummary, error) }`
  - `func New(fetchFn) *Resolver`
  - `func (r *Resolver) Resolve(deps []SkillSummary) (map[string]string, error)`:
    1. Build `map[id]version` starting from input deps
    2. For each dep, fetch its transitive deps
    3. For each transitive dep: if id exists in map and current.version > existing.version, update
    4. Track visited set to avoid cycles
    5. BFS/queue-based traversal until all transitive deps resolved
  - Follows design doc Section 7.7 MVS algorithm
- [ ] **Run test** → PASS
- [ ] **Conditional commit**

---

### Task 5: Installer (Local Installation Management)

**Files:** `pkg/installer/installer.go`, `pkg/installer/installer_test.go`

- [ ] **Write tests:**

```go
func TestInstallPath(t *testing.T) {
    t.Setenv("SKILLHUB_HOME", "/tmp/skillhub")
    p := installer.InstallPath("github.com/acme/clawhub/social/publish-post", "v1.4.2")
    expected := "/tmp/skillhub/skills/github.com/acme/clawhub/social/publish-post/v1.4.2"
    if p != expected { t.Fatalf("expected %s, got %s", expected, p) }
}

func TestIsInstalled(t *testing.T) {
    dir := t.TempDir()
    t.Setenv("SKILLHUB_HOME", dir)
    if installer.IsInstalled("github.com/acme/A", "v1.0.0") {
        t.Fatal("should not be installed yet")
    }
    os.MkdirAll(installer.InstallPath("github.com/acme/A", "v1.0.0"), 0755)
    if !installer.IsInstalled("github.com/acme/A", "v1.0.0") {
        t.Fatal("should be installed now")
    }
}
```

- [ ] **Run test:** → FAIL
- [ ] **Implement installer.go:**
  - `func HomeDir() string` — read `$SKILLHUB_HOME`, default to `$HOME/.skillhub`
  - `func InstallPath(id, version string) string` — build path per Section 8.1
  - `func IsInstalled(id, version string) bool` — check directory exists
  - `func Ensure(id, version string) error` — check local, run install if missing
  - `func Install(id, version string) error` — git clone from remote to local dir, install deps
  - `type Installer struct` — holds VCS client, Resolver, Loader references
- [ ] **Run test** → PASS
- [ ] **Conditional commit**

---

### Task 6: Loader

**Files:** `pkg/loader/loader.go`, `pkg/loader/loader_test.go`

- [ ] **Write tests (uses testdata from parser + installer):**

```go
func TestLoadRootSkill(t *testing.T) {
    dir := filepath.Join("..", "parser", "testdata", "root-skill")
    skill, err := loader.LoadRoot(dir, "v1.0.0")
    if err != nil { t.Fatal(err) }
    if skill.ID != "github.com/acme/clawhub/social/publish-post" { t.Fatal("bad id") }
    if skill.Version != "v1.0.0" { t.Fatal("bad version") }
    if len(skill.SubSkills) != 2 { t.Fatal("expected 2 sub skills") }
    if len(skill.Deps.Tools) != 2 { t.Fatal("expected 2 tools") }
}

func TestLoadSubSkill(t *testing.T) {
    dir := filepath.Join("..", "parser", "testdata", "root-skill")
    skill, err := loader.LoadSub(dir, "sub-a", "github.com/acme/clawhub/social/publish-post", "v1.0.0")
    if err != nil { t.Fatal(err) }
    if skill.ID != "github.com/acme/clawhub/social/publish-post/sub-a" { t.Fatal("bad sub id") }
}
```

- [ ] **Run test:** → FAIL
- [ ] **Implement loader.go:**
  - `func LoadRoot(dir, version string) (*types.Skill, error)`:
    1. `parser.ParseRoot(dir)` → get root info + sub paths
    2. For each sub path, parse sub SKILL.md to build `SkillSummary`
    3. Return full `Skill`
  - `func LoadSub(rootDir, subPath, parentID, version string) (*types.Skill, error)`:
    1. Parse sub SKILL.md
    2. Return `Skill` with empty SubSkills/Deps
- [ ] **Run test** → PASS
- [ ] **Conditional commit**

---

### Task 7: Discovery Center (PostgreSQL)

**Files:** `pkg/discovery/discovery.go`, `pkg/discovery/discovery_test.go`
**Depends on:** `github.com/jackc/pgx/v5`

- [ ] **Write test (uses test PostgreSQL or in-memory):**

```go
// Use pgx with a test postgres, or mock the query interface
func TestSearchByID(t *testing.T) {
    // Setup: insert a skill
    // Search by exact ID
    // Verify result
}

func TestSearchByDescription(t *testing.T) {
    // Search by description regex
    // Verify regex matching works
}

func TestSearchByTag(t *testing.T) {
    // Search by tag regex
}

func TestSearchPrefixID(t *testing.T) {
    // Search by prefix ID match
}

func TestRegisterAndApprove(t *testing.T) {
    // Register a skill, verify it's searchable
    // Then approve it
}
```

For test, use `pgx` connecting to `postgres://localhost:5432/skillhub_test` (read from env, skip if not available):

```go
func connectTestDB(t *testing.T) *pgxpool.Pool {
    t.Helper()
    dsn := os.Getenv("SKILLHUB_TEST_DSN")
    if dsn == "" {
        t.Skip("SKILLHUB_TEST_DSN not set")
    }
    pool, err := pgxpool.New(context.Background(), dsn)
    if err != nil { t.Fatal(err) }
    t.Cleanup(pool.Close)
    return pool
}
```

- [ ] **Run test:** `go test ./pkg/discovery/ -v` → FAIL (no package)
- [ ] **Implement discovery.go:**
  - `type Discovery struct { pool *pgxpool.Pool }`
  - `func New(pool *pgxpool.Pool) *Discovery`
  - `func (d *Discovery) Init(ctx) error` — auto-create table
  - `func (d *Discovery) Search(req SearchRequest) ([]SkillSummary, error)` — build dynamic SQL:
    - `id` → `id = $1 OR id LIKE $1 || '/%'` (exact or prefix)
    - `description` → `description ~* $1`
    - `tag` → `EXISTS (SELECT 1 FROM unnest(tags) t WHERE t ~* $1)`
    - `approved = true` filter
    - `LIMIT` from req.Limit (default 20)
  - `func (d *Discovery) Register(skill SkillSummary) error` — `INSERT ... ON CONFLICT (id) DO UPDATE`
  - `func (d *Discovery) Approve(id string) error` — `UPDATE skills SET approved = true WHERE id = $1`

Table schema:
```sql
CREATE TABLE IF NOT EXISTS skills (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    version     TEXT NOT NULL DEFAULT '',
    tags        TEXT[] NOT NULL DEFAULT '{}',
    approved    BOOLEAN NOT NULL DEFAULT false,
    source      TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

- [ ] **Run test** → PASS (or SKIP if no Postgres)
- [ ] **Conditional commit**

---

### Task 8: Security Scanning

**Files:** `pkg/security/scanner.go`, `pkg/security/rule_scanner.go`, `pkg/security/virustotal.go`, `pkg/security/scanner_test.go`

- [ ] **Write tests:**

```go
func TestRuleScanner_DetectBase64(t *testing.T) {
    content := "echo 'dGVzdA==' | base64 -d | sh"
    scanner := security.NewRuleScanner()
    result, err := scanner.ScanContent("test.sh", content)
    if err != nil { t.Fatal(err) }
    if result.Passed { t.Fatal("should detect base64 decode + exec") }
}

func TestRuleScanner_SafeFile(t *testing.T) {
    content := "print('hello world')"
    scanner := security.NewRuleScanner()
    result, err := scanner.ScanContent("test.py", content)
    if err != nil { t.Fatal(err) }
    if !result.Passed { t.Fatal("safe file should pass, got: %v", result.Issues) }
}

func TestRuleScanner_DetectHardcodedToken(t *testing.T) {
    content := "api_key = 'sk-abc123def456'"
    scanner := security.NewRuleScanner()
    result, err := scanner.ScanContent(".env", content)
    if err != nil { t.Fatal(err) }
    if result.Passed { t.Fatal("should detect hardcoded token") }
}

func TestChainScanner(t *testing.T) {
    scanner := security.NewChainScanner()
    scanner.Add(security.NewRuleScanner())
    // Scanner with no scanners passes by default
}
```

- [ ] **Run test:** `go test ./pkg/security/ -v` → FAIL
- [ ] **Implement scanner.go:**

```go
package security

type ScanResult struct {
    Passed  bool
    Issues  []string
}

type Scanner interface {
    Scan(path string) (*ScanResult, error)
}
```

- [ ] **Implement rule_scanner.go:**
  - `RuleScanner` with patterns for:
    - `base64.*-d.*\|.*(bash|sh)` — base64 decode to shell
    - `curl\s+(https?://\d+\.\d+\.\d+\.\d+)` — curl to IP address
    - `wget\s+(https?://\d+\.\d+\.\d+\.\d+)` — wget to IP address
    - `(?:sk-|api[_-]?key|secret|token)\s*[:=]\s*['\"][a-zA-Z0-9_\-]{16,}` — hardcoded secrets
    - `(?:>/dev/tcp/|exec\s+\d+>&1)` — reverse shell
    - `rm\s+-rf\s+/\s*$` — destructive file ops
    - `chmod\s+777` — insecure permissions
  - `Scan(path)` reads file, runs all patterns, collects issues
  - `ScanContent(name, content string)` for testing

- [ ] **Implement virustotal.go:**
  - `type VirusTotalScanner struct { apiKey string; client *http.Client }`
  - `func NewVirusTotalScanner(apiKey string) *VirusTotalScanner`
  - `func (s *VirusTotalScanner) Scan(path string) (*ScanResult, error)`:
    - If no API key, return `{Passed: true}`
    - Upload file to VT API v3, poll for analysis, return results
    - VT API: `POST /api/v3/files` → `GET /api/v3/analyses/{id}`

- [ ] **Implement chain_scanner.go:**
  - `type ChainScanner struct { scanners []Scanner }`
  - Aggregate results: all must pass

- [ ] **Run test** → PASS
- [ ] **Conditional commit**

---

### Task 9: MCP Server

**Files:** `pkg/mcp/server.go`, `pkg/mcp/server_test.go`

- [ ] **Write tests:**

```go
func TestHandleToolsList(t *testing.T) {
    msg := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
    server := mcp.New(nil)
    resp := server.Handle(msg)
    // verify response contains skillhub_search and skillhub_load
}

func TestHandleToolsCall_Search(t *testing.T) {
    mock := &mockTools{}
    msg := json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"skillhub_search","arguments":{"id":"test"}}}`)
    server := mcp.New(mock)
    resp := server.Handle(msg)
    // verify response
}
```

- [ ] **Run test:** → FAIL
- [ ] **Implement server.go:**
  - Standard MCP protocol over stdio (JSON-RPC 2.0)
  - `func New(tools types.SkillHubTools) *Server`
  - `func (s *Server) Run(ctx) error` — read JSON-RPC from stdin, write to stdout
  - Handle `tools/list` → return two tool definitions
  - Handle `tools/call` → dispatch to `skillhub_search` or `skillhub_load`, return result or error
  - Tool input/output schema per design doc Section 4.3
  - Use `encoding/json` for all message handling

- [ ] **Run test** → PASS
- [ ] **Conditional commit**

---

### Task 10: CLI Entry Point

**Files:** `cmd/skillhub/main.go`

- [ ] **Write integration test or verify manually:**
  - `go build ./cmd/skillhub/` should succeed
  - `skillhub serve` should start MCP stdio server

- [ ] **Implement main.go:**
  - Parse CLI flags: `serve` (default), `search`, `load`, `install`
  - `skillhub serve` — create Discovery (PostgreSQL), Loader, Installer, Security scanners, wire them into a `SkillHubCore`, pass to MCP server, run
  - `skillhub search <query>` — connect to Postgres, search, print results
  - `skillhub load <id>` — load and print skill JSON
  - `SkillHubCore` implements `types.SkillHubTools`:
    - `Search` → Discovery.Search, filter approved-only
    - `Load` → Installer.Ensure, then Loader.Load

- [ ] **Build:** `go build ./cmd/skillhub/` → PASS
- [ ] **Conditional commit**

---

### Task 11: AGENTS.md

**Files:** Create `AGENTS.md` at project root

- [ ] **Write AGENTS.md** with:
  - Project overview
  - Architecture summary
  - How to build and test
  - How to run the MCP server
  - PostgreSQL setup instructions
  - Key conventions (Go style, package layout)
  - Important design decisions
  - Testing approach
  - Environment variables (`SKILLHUB_HOME`, `SKILLHUB_DATABASE_URL`, `VT_API_KEY`)

- [ ] **Conditional commit**

---

### Task 12: End-to-End Verification

- [ ] **Build all:** `go build ./cmd/skillhub/ && go build ./...`
- [ ] **Run all tests:** `go test ./... -v`
- [ ] **Fix any failures**
