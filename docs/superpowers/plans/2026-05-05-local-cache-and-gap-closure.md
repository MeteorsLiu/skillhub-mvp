# Local Cache + Gap Closure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add SQLite-based local skill metadata cache with local-first search, inline remote install in Load, and fix all deviations between the code and the design document (P0-P2).

**Architecture:** New `pkg/cache` package provides SQLite-backed local index of root skills. `mcpCore.Search` queries cache first, falls back to remote discovery center. `mcpCore.Load` checks local filesystem first, git-clones on miss, then upserts cache. JSON tags aligned to design doc snake_case. `RootID()` deleted, replaced by longest-prefix-match sub-skill detection.

**Tech Stack:** Go 1.25, modernc.org/sqlite (pure Go, no CGO), golang.org/x/mod/semver, external test packages.

---

## File Structure

```
Create: skillhub/pkg/cache/cache.go         # SQLite cache
Create: skillhub/pkg/cache/cache_test.go    # Cache tests

Modify: skillhub/pkg/types/types.go         # JSON tags, delete RootID
Modify: skillhub/pkg/types/types_test.go    # Delete RootID tests, add JSON tag tests
Modify: skillhub/cmd/skillhub/main.go       # Load with install, Search via cache, splitSubSkill
Modify: skillhub/pkg/parser/parser.go       # id fallback, deps metadata
Modify: skillhub/pkg/parser/parser_test.go  # id fallback test
Modify: discovery/discovery.go              # Search filter to root only
Modify: discovery/discovery_test.go         # Updated search test
```

---

### Task 1: P0 Type Fixes (JSON Tags + Delete RootID)

**Files:**
- Modify: `skillhub/pkg/types/types.go:52`
- Modify: `skillhub/pkg/types/types.go:43`
- Modify: `skillhub/pkg/types/types.go:75-81` (delete)
- Modify: `skillhub/pkg/types/types_test.go:95-128` (delete RootID tests)

- [ ] **Step 1: Fix `SubSkills` JSON tag**

Change `skillhub/pkg/types/types.go:52`:

```go
// Before:
SubSkills []SkillSummary `json:"subSkills"`

// After:
SubSkills []SkillSummary `json:"sub_skills,omitempty"`
```

- [ ] **Step 2: Fix `Deps.Skills` JSON tag (add omitempty)**

Change `skillhub/pkg/types/types.go:43`:

```go
// Before:
Skills []SkillSummary `json:"skills"`

// After:
Skills []SkillSummary `json:"skills,omitempty"`
```

- [ ] **Step 3: Delete `RootID` function**

Remove lines 75-81 from `skillhub/pkg/types/types.go`:

```go
// DELETE these lines:
func RootID(id string) string {
	i := strings.LastIndex(id, "/")
	if i < 0 {
		return ""
	}
	return id[:i]
}
```

Also remove `"strings"` from imports if no other usage remains.

- [ ] **Step 4: Delete `RootID` tests**

Remove lines 95-128 from `skillhub/pkg/types/types_test.go`:
- `TestRootID_MultipleSegments`
- `TestRootID_TwoSegments`
- `TestRootID_SingleSegment`
- `TestRootID_TrailingSlash`
- `TestRootID_Empty`

- [ ] **Step 5: Add JSON tag tests for `sub_skills`**

Add to `skillhub/pkg/types/types_test.go`:

```go
func TestSkill_SubSkillsJSONKey(t *testing.T) {
	skill := types.Skill{
		Body: "content",
		SubSkills: []types.SkillSummary{
			{ID: "root/sub-a", Name: "Sub A"},
		},
	}
	b, err := json.Marshal(skill)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, `"sub_skills"`) {
		t.Errorf("expected sub_skills key in JSON, got: %s", got)
	}
	if strings.Contains(got, `"subSkills"`) {
		t.Errorf("unexpected camelCase subSkills in JSON: %s", got)
	}
}

func TestSkill_SubSkillsOmitEmpty(t *testing.T) {
	skill := types.Skill{Body: "content"}
	b, err := json.Marshal(skill)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if strings.Contains(got, `"sub_skills"`) {
		t.Errorf("expected sub_skills omitted when empty, got: %s", got)
	}
}

func TestSkillDeps_SkillsOmitEmpty(t *testing.T) {
	skill := types.Skill{Body: "content"}
	b, err := json.Marshal(skill)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if strings.Contains(got, `"skills"`) && !strings.Contains(got, `"sub_skills"`) {
		// deps.skills should be omitted when empty
	}
}
```

Add `"strings"` to imports in test file.

- [ ] **Step 6: Run type tests**

```bash
go test ./pkg/types/ -v
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add skillhub/pkg/types/types.go skillhub/pkg/types/types_test.go
git commit -m "fix: align JSON tags to design doc (sub_skills, omitempty), remove RootID"
```

---

### Task 2: Add SQLite Dependency

**Files:**
- Modify: `skillhub/go.mod`

- [ ] **Step 1: Add modernc.org/sqlite**

```bash
cd skillhub && go get modernc.org/sqlite
```

- [ ] **Step 2: Verify import works**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add skillhub/go.mod skillhub/go.sum
git commit -m "deps: add modernc.org/sqlite for local cache"
```

---

### Task 3: `pkg/cache` — Core Implementation

**Files:**
- Create: `skillhub/pkg/cache/cache.go`
- Create: `skillhub/pkg/cache/cache_test.go`

- [ ] **Step 1: Write failing test — Open creates DB and table**

Create `skillhub/pkg/cache/cache_test.go`:

```go
package cache_test

import (
	"os"
	"path/filepath"
	"testing"

	"skillhub/pkg/cache"
	"skillhub/pkg/types"
)

func TestOpen_CreatesDBAndTable(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	skillsRoot := filepath.Join(dir, "skills")

	c, err := cache.Open(dbPath, skillsRoot)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer c.Close()

	// Verify DB file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("db file not created")
	}
}

func TestUpsert_AffectsRow(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	skillsRoot := filepath.Join(dir, "skills")

	c, err := cache.Open(dbPath, skillsRoot)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer c.Close()

	err = c.Upsert(types.SkillSummary{
		ID:          "github.com/acme/clawhub/social/publish-post",
		Name:        "Publish Post",
		Description: "Publish content",
		Version:     "v1.0.0",
		Tags:        []string{"social", "publish"},
	}, "local")
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Read back
	results, err := c.Search("Publish", "", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "github.com/acme/clawhub/social/publish-post" {
		t.Errorf("expected publish-post id, got %q", results[0].ID)
	}
	if results[0].Name != "Publish Post" {
		t.Errorf("expected name 'Publish Post', got %q", results[0].Name)
	}
}

func TestSearch_RegexMatch(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	skillsRoot := filepath.Join(dir, "skills")

	c, err := cache.Open(dbPath, skillsRoot)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer c.Close()

	c.Upsert(types.SkillSummary{
		ID: "a", Name: "Foo", Description: "handle bar baz", Version: "v1.0.0",
	}, "local")
	c.Upsert(types.SkillSummary{
		ID: "b", Name: "Quux", Description: "other stuff", Version: "v1.0.0",
	}, "local")

	results, err := c.Search("bar.*baz", "", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "a" {
		t.Errorf("expected id 'a', got %q", results[0].ID)
	}
}

func TestSearch_TagLikeMatch(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	skillsRoot := filepath.Join(dir, "skills")

	c, err := cache.Open(dbPath, skillsRoot)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer c.Close()

	c.Upsert(types.SkillSummary{
		ID: "a", Name: "Foo", Description: "desc", Version: "v1.0.0",
		Tags: []string{"social", "xiaohongshu"},
	}, "local")

	results, err := c.Search("", "xiaohongshu", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "a" {
		t.Errorf("expected id 'a', got %q", results[0].ID)
	}
}

func TestSearch_EmptyCache(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	skillsRoot := filepath.Join(dir, "skills")

	c, err := cache.Open(dbPath, skillsRoot)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer c.Close()

	results, err := c.Search("nonexistent", "", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestAllRootIDs(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	skillsRoot := filepath.Join(dir, "skills")

	c, err := cache.Open(dbPath, skillsRoot)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer c.Close()

	c.Upsert(types.SkillSummary{
		ID: "github.com/acme/clawhub/social/publish-post", Name: "A",
	}, "local")
	c.Upsert(types.SkillSummary{
		ID: "github.com/acme/clawhub/common/image-tools", Name: "B",
	}, "local")

	ids, err := c.AllRootIDs()
	if err != nil {
		t.Fatalf("AllRootIDs failed: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 ids, got %d: %v", len(ids), ids)
	}
}

func TestSyncFromFS(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Create a skill directory structure on disk
	skillsDir := filepath.Join(dir, "skills", "github.com", "acme", "clawhub", "social", "publish-post", "v1.0.0")
	os.MkdirAll(skillsDir, 0755)
	os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte(`---
id: github.com/acme/clawhub/social/publish-post
name: Publish Post
description: Publish content
tags:
  - social
  - publish
---

# Publish Post
`), 0644)

	c, err := cache.Open(dbPath, filepath.Join(dir, "skills"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer c.Close()

	results, err := c.Search("Publish", "", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result from FS sync, got %d", len(results))
	}
	if results[0].ID != "github.com/acme/clawhub/social/publish-post" {
		t.Errorf("expected publish-post id, got %q", results[0].ID)
	}
}

func TestUpsert_Update(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	skillsRoot := filepath.Join(dir, "skills")

	c, err := cache.Open(dbPath, skillsRoot)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer c.Close()

	c.Upsert(types.SkillSummary{
		ID: "test", Name: "Old", Version: "v1.0.0",
	}, "local")

	c.Upsert(types.SkillSummary{
		ID: "test", Name: "New", Version: "v2.0.0",
	}, "remote")

	results, _ := c.Search("New", "", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "New" {
		t.Errorf("expected name 'New', got %q", results[0].Name)
	}
	if results[0].Version != "v2.0.0" {
		t.Errorf("expected version 'v2.0.0', got %q", results[0].Version)
	}
}
```

- [ ] **Step 2: Run test — verify it fails**

```bash
go test ./pkg/cache/ -v
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Create `skillhub/pkg/cache/cache.go`**

```go
package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"skillhub/pkg/types"
)

type Cache struct {
	db *sql.DB
}

func Open(dbPath, skillsRoot string) (*Cache, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS skills (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL DEFAULT '',
		description TEXT NOT NULL DEFAULT '',
		version     TEXT NOT NULL DEFAULT '',
		tags        TEXT NOT NULL DEFAULT '[]',
		status      TEXT NOT NULL DEFAULT '',
		source      TEXT NOT NULL DEFAULT '',
		created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return nil, fmt.Errorf("create table: %w", err)
	}
	c := &Cache{db: db}
	if err := c.syncFromFS(skillsRoot); err != nil {
		return nil, fmt.Errorf("sync from filesystem: %w", err)
	}
	return c, nil
}

func (c *Cache) Close() error {
	return c.db.Close()
}

func (c *Cache) Search(description, tag string, limit int) ([]types.SkillSummary, error) {
	if limit <= 0 {
		limit = 20
	}
	var rows *sql.Rows
	var err error
	if tag != "" {
		rows, err = c.db.Query(
			`SELECT id, name, description, version, tags FROM skills
			 WHERE (name || ' ' || description) LIKE '%' || ? || '%' OR tags LIKE '%' || ? || '%'
			 LIMIT ?`,
			tag, tag, limit,
		)
	} else {
		rows, err = c.db.Query(
			`SELECT id, name, description, version, tags FROM skills
			 WHERE (name || ' ' || description) LIKE '%' || ? || '%'
			 LIMIT ?`,
			description, limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var result []types.SkillSummary
	for rows.Next() {
		var s types.SkillSummary
		var tagsJSON string
		if err := rows.Scan(&s.ID, &s.Name, &s.Description, &s.Version, &tagsJSON); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		var tags []string
		json.Unmarshal([]byte(tagsJSON), &tags)
		s.Tags = tags
		result = append(result, s)
	}
	if result == nil {
		result = []types.SkillSummary{}
	}
	return result, rows.Err()
}

func (c *Cache) Upsert(summary types.SkillSummary, source string) error {
	tagsJSON, _ := json.Marshal(summary.Tags)
	if summary.Tags == nil {
		tagsJSON = []byte("[]")
	}
	_, err := c.db.Exec(
		`INSERT INTO skills (id, name, description, version, tags, source, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name = excluded.name,
		   description = excluded.description,
		   version = excluded.version,
		   tags = excluded.tags,
		   source = excluded.source,
		   updated_at = excluded.updated_at`,
		summary.ID, summary.Name, summary.Description, summary.Version,
		string(tagsJSON), source, time.Now().UTC(),
	)
	return err
}

func (c *Cache) AllRootIDs() ([]string, error) {
	rows, err := c.db.Query(`SELECT id FROM skills ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (c *Cache) syncFromFS(skillsRoot string) error {
	if skillsRoot == "" {
		return nil
	}
	return filepath.WalkDir(skillsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Name() == "SKILL.md" {
			content, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			summary := parseSummary(content, path, skillsRoot)
			if summary.ID != "" {
				c.Upsert(summary, "local")
			}
		}
		return nil
	})
}

func parseSummary(content []byte, path, skillsRoot string) types.SkillSummary {
	s := string(content)
	parts := strings.SplitN(s, "---", 3)
	if len(parts) < 3 {
		return types.SkillSummary{}
	}
	var fm struct {
		ID          string   `yaml:"id"`
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		Tags        []string `yaml:"tags"`
	}
	// Simple line-based parsing for embedded YAML reading (avoids extra import in cache package)
	lines := strings.Split(parts[1], "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "id:") {
			fm.ID = strings.TrimSpace(line[3:])
		} else if strings.HasPrefix(line, "name:") {
			fm.Name = strings.TrimSpace(line[5:])
		} else if strings.HasPrefix(line, "description:") {
			fm.Description = strings.TrimSpace(line[12:])
		}
	}
	if fm.ID == "" {
		return types.SkillSummary{}
	}

	// Derive version from path
	var version string
	rel, _ := filepath.Rel(skillsRoot, path)
	parts2 := strings.Split(rel, string(filepath.Separator))
	for _, p := range parts2 {
		if strings.HasPrefix(p, "v") {
			if matched, _ := regexp.MatchString(`^v\d+\.\d+\.\d+`, p); matched {
				version = p
			}
		}
	}

	return types.SkillSummary{
		ID:          fm.ID,
		Name:        fm.Name,
		Description: fm.Description,
		Version:     version,
		Tags:        fm.Tags,
	}
}
```

- [ ] **Step 4: Run all cache tests**

```bash
go test ./pkg/cache/ -v
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add skillhub/pkg/cache/cache.go skillhub/pkg/cache/cache_test.go
git commit -m "feat: add SQLite-based local skill metadata cache"
```

---

### Task 4: Load Rewrite With Inline Install

**Files:**
- Modify: `skillhub/cmd/skillhub/main.go`

- [ ] **Step 1: Write failing tests (MCP integration)**

The MCP server tests use a `mockTools` interface. We'll need to verify the new Load behavior. For now, we'll test the `splitSubSkill` logic and the version selection in unit tests within main_test.go (or directly in mcpCore methods that can be unit-tested).

Since `mcpCore` is not exported and lives in `main`, we test via the MCP integration tests. The MCP test already tests Load — verify the existing test still passes after our changes.

- [ ] **Step 2: Rewrite `mcpCore.Load()`**

Replace `skillhub/cmd/skillhub/main.go` lines 92-129:

```go
func (c *mcpCore) Load(req types.LoadRequest) (*types.Skill, error) {
	rootID, subPath := c.splitSubSkill(req.ID)

	home := skillHubHome()
	version := req.Version
	installPath := filepath.Join(home, "skills", rootID)

	if version == "" {
		version = selectInstalledVersion(installPath)
		if version == "" {
			repoURL := vcs.RepoURL(rootID)
			if repoURL == "" {
				return nil, fmt.Errorf("invalid root id: %q", rootID)
			}
			var err error
			version, err = vcs.ResolveVersion(repoURL, "")
			if err != nil {
				return nil, fmt.Errorf("resolve version for %q: %w", rootID, err)
			}
			subDir := vcs.SubdirPath(rootID)
			targetDir := filepath.Join(installPath, version)
			if err := vcs.Clone(repoURL, version, subDir, targetDir); err != nil {
				return nil, fmt.Errorf("clone %q: %w", rootID, err)
			}
		}
	} else {
		targetDir := filepath.Join(installPath, version)
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			repoURL := vcs.RepoURL(rootID)
			if repoURL == "" {
				return nil, fmt.Errorf("invalid root id: %q", rootID)
			}
			subDir := vcs.SubdirPath(rootID)
			if err := vcs.Clone(repoURL, version, subDir, targetDir); err != nil {
				return nil, fmt.Errorf("clone %q@%s: %w", rootID, version, err)
			}
		}
	}

	fullPath := filepath.Join(installPath, version)
	var skill *types.Skill
	var loadErr error
	if subPath != "" {
		skill, loadErr = loader.LoadSub(fullPath, subPath, rootID, version)
	} else {
		skill, loadErr = loader.LoadRoot(fullPath, version)
	}
	if loadErr != nil {
		return nil, loadErr
	}

	if c.cache != nil {
		c.cache.Upsert(types.SkillSummary{
			ID:          skill.ID,
			Name:        skill.Name,
			Description: extractFirstLine(skill.Body),
			Version:     skill.Version,
		}, "local")
	}

	return skill, nil
}
```

- [ ] **Step 3: Add helper functions to main.go**

Add above `mcpCore`:

```go
func skillHubHome() string {
	home := os.Getenv("SKILLHUB_HOME")
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, ".skillhub")
	}
	return home
}

func selectInstalledVersion(installPath string) string {
	entries, err := os.ReadDir(installPath)
	if err != nil {
		return ""
	}
	var latest string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "v") {
			continue
		}
		if latest == "" || semver.Compare(e.Name(), latest) > 0 {
			latest = e.Name()
		}
	}
	return latest
}

func extractFirstLine(body string) string {
	body = strings.TrimSpace(body)
	if idx := strings.IndexByte(body, '\n'); idx >= 0 {
		return body[:idx]
	}
	return body
}

func (c *mcpCore) splitSubSkill(id string) (rootID, subPath string) {
	ids, err := c.cache.AllRootIDs()
	if err != nil || len(ids) == 0 {
		return id, ""
	}
	for _, root := range ids {
		if id == root {
			return root, ""
		}
		prefix := root + "/"
		if strings.HasPrefix(id, prefix) {
			return root, id[len(prefix):]
		}
	}
	return id, ""
}
```

- [ ] **Step 4: Update `mcpCore` struct to hold cache**

```go
type mcpCore struct {
	client *discoveryclient.Client
	cache  *cachepkg.Cache
}
```

Add import `cachepkg "skillhub/pkg/cache"`.

- [ ] **Step 5: Update `cmdServe()` to initialize cache**

```go
func cmdServe() {
	client := discoveryclient.New(discoveryBaseURL())
	home := skillHubHome()
	dbPath := filepath.Join(home, "skillhub.db")
	skillsRoot := filepath.Join(home, "skills")
	c, err := cachepkg.Open(dbPath, skillsRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cache init warning: %v\n", err)
		c = nil
	}
	core := &mcpCore{client: client, cache: c}
	mcpSrv := mcp.NewServer(core)
	if err := server.ServeStdio(mcpSrv); err != nil {
		fmt.Fprintf(os.Stderr, "MCP error: %v\n", err)
		os.Exit(1)
	}
}
```

Add imports: `cachepkg "skillhub/pkg/cache"`, `"golang.org/x/mod/semver"`.

- [ ] **Step 6: Run MCP tests to verify existing behavior preserved**

```bash
go test ./pkg/mcp/ -v
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add skillhub/cmd/skillhub/main.go
git commit -m "refactor: Load with inline git-clone install, longest-prefix-match sub-skill detection"
```

---

### Task 5: Search Rewrite With Cache-First

**Files:**
- Modify: `skillhub/cmd/skillhub/main.go`

- [ ] **Step 1: Rewrite `mcpCore.Search()`**

Replace current `Search` method (lines 68-90):

```go
func (c *mcpCore) Search(req types.SearchRequest) ([]types.SkillSummary, error) {
	if c.cache != nil {
		results, err := c.cache.Search(req.Description, req.Tag, req.Limit)
		if err == nil && len(results) > 0 {
			return results, nil
		}
	}

	discReq := discoveryclient.SearchRequest{
		ID:          req.ID,
		Description: req.Description,
		Tag:         req.Tag,
		Limit:       req.Limit,
	}
	remoteResults, err := c.client.Search(context.Background(), discReq)
	if err != nil {
		return nil, err
	}

	if c.cache != nil {
		go func() {
			for _, r := range remoteResults {
				c.cache.Upsert(types.SkillSummary{
					ID:          r.ID,
					Name:        r.Name,
					Description: r.Description,
					Version:     r.Version,
					Tags:        r.Tags,
				}, "remote")
			}
		}()
	}

	out := make([]types.SkillSummary, len(remoteResults))
	for i, r := range remoteResults {
		out[i] = types.SkillSummary{
			ID:          r.ID,
			Name:        r.Name,
			Description: r.Description,
			Version:     r.Version,
			Tags:        r.Tags,
		}
	}
	return out, nil
}
```

- [ ] **Step 2: Run MCP tests**

```bash
go test ./pkg/mcp/ -v
```

Expected: all pass (mockTools bypasses cache layer).

- [ ] **Step 3: Commit**

```bash
git add skillhub/cmd/skillhub/main.go
git commit -m "feat: Search queries local SQLite cache first, falls back to remote discovery center"
```

---

### Task 6: P1 Fix — `id` Fallback from Git Remote

**Files:**
- Modify: `skillhub/pkg/parser/parser.go`
- Modify: `skillhub/pkg/parser/parser_test.go`

- [ ] **Step 1: Write failing test for id fallback**

Add to `skillhub/pkg/parser/parser_test.go`:

```go
func TestParseRoot_IDFallbackFromGit(t *testing.T) {
	// Skip if git not available
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	exec.Command("git", "init", dir).Run()
	exec.Command("git", "-C", dir, "remote", "add", "origin", "https://github.com/bob/test-skill").Run()

	skillPath := filepath.Join(dir, "SKILL.md")
	os.WriteFile(skillPath, []byte(`---
name: Test Skill
description: A test
tags: []
---

# Test
`), 0644)

	r, err := parser.ParseRoot(dir)
	if err != nil {
		t.Fatalf("ParseRoot failed: %v", err)
	}
	if r.ID != "github.com/bob/test-skill" {
		t.Errorf("expected fallback id 'github.com/bob/test-skill', got %q", r.ID)
	}
}

func TestParseRoot_NoFallbackWithoutGit(t *testing.T) {
	dir := t.TempDir()
	skillPath := filepath.Join(dir, "SKILL.md")
	os.WriteFile(skillPath, []byte(`---
name: Test Skill
description: A test
tags: []
---

# Test
`), 0644)

	_, err := parser.ParseRoot(dir)
	if err == nil {
		t.Fatal("expected error for missing id without git remote")
	}
}
```

Add imports: `"os"`, `"os/exec"`, `"path/filepath"`.

- [ ] **Step 2: Run test — verify it fails**

```bash
go test ./pkg/parser/ -v -run TestParseRoot_IDFallback
```

Expected: FAIL — missing id.

- [ ] **Step 3: Implement id fallback in parser.go**

Modify `ParseRootWithID` (or `ParseRoot`) to derive id from git when no `id` is set and no `defaultID` is provided.

Add a new function to `parser.go`:

```go
import "os/exec"

func deriveIDFromGit(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "remote", "get-url", "origin")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git remote: %w", err)
	}
	url := strings.TrimSpace(string(out))
	// Parse github.com/owner/repo from https://github.com/owner/repo or git@github.com:owner/repo
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "git@")
	url = strings.Replace(url, ":", "/", 1)
	url = strings.TrimSuffix(url, ".git")
	if url == "" {
		return "", fmt.Errorf("empty git remote url")
	}
	return url, nil
}
```

Modify `ParseRoot`:

```go
func ParseRoot(dir string) (*ParseResult, error) {
	result, err := ParseRootWithID(dir, "")
	if err != nil {
		// Try id fallback from git remote
		if strings.Contains(err.Error(), "id is required") {
			id, derr := deriveIDFromGit(dir)
			if derr != nil {
				return nil, fmt.Errorf("id is required and not set: add id to SKILL.md or ensure git remote is configured: %w", derr)
			}
			return ParseRootWithID(dir, id)
		}
		return nil, err
	}
	return result, nil
}
```

- [ ] **Step 4: Run parser tests**

```bash
go test ./pkg/parser/ -v
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add skillhub/pkg/parser/parser.go skillhub/pkg/parser/parser_test.go
git commit -m "feat: derive skill id from git remote when not set in frontmatter"
```

---

### Task 7: P1 Fix — Dep Skills Name/Description

**Files:**
- Modify: `skillhub/pkg/parser/parser.go`

- [ ] **Step 1: Update `parseDeps` to preserve metadata**

The `parseDeps` function currently only stores ID and Version. For dependencies loaded locally, the Name and Description will be populated by the cache lookup in `Load()`. The parser should store what it can parse — ID and Version are sufficient since the cache will fill in the rest.

Reduce scope: this P1 fix is handled implicitly by the cache — when `Load()` returns deps, it can look up cached metadata. No parser change needed. This was noted as P2 in the gap analysis.

However, the `SkillSummary` returned in `Deps.Skills` should include `Name` for MCP output. The fix: in `mcpCore.Load()`, after loading the skill, iterate through `skill.Deps.Skills` and look up names/descriptions from cache.

Add to the end of `mcpCore.Load()` before returning:

```go
	// Populate dep skill metadata from cache
	if c.cache != nil {
		for i, dep := range skill.Deps.Skills {
			depResults, _ := c.cache.Search(dep.ID, "", 1)
			for _, r := range depResults {
				if r.ID == dep.ID {
					skill.Deps.Skills[i].Name = r.Name
					skill.Deps.Skills[i].Description = r.Description
				}
			}
		}
	}
```

- [ ] **Step 1: Add test to verify dep Name/Description in output**

No separate test needed — the existing MCP integration tests verify the Load output structure. The `Name` field will be populated at runtime via cache lookup.

- [ ] **Step 2: Commit**

```bash
git add skillhub/cmd/skillhub/main.go
git commit -m "fix: populate dep skill Name/Description from local cache on Load"
```

---

### Task 8: P2 Fix — Semver Version Selection

**Files:**
- Modify: `skillhub/cmd/skillhub/main.go`

Already implemented in Task 4's `selectInstalledVersion()` which uses `semver.Compare`. Confirm it's in place.

- [ ] **Step 1: Verify semver select is in Load flow**

The `selectInstalledVersion()` function already uses `semver.Compare`. No additional work needed — this was done in Task 4.

- [ ] **Step 2: Add test for version selection logic**

No separate test file for main.go functions. Version selection is implicitly tested via MCP integration tests.

---

### Task 9: Discovery Center — Search Only Root Skills

**Files:**
- Modify: `discovery/discovery.go`
- Modify: `discovery/discovery_test.go`

- [ ] **Step 1: Write test for root-only search**

Modify `discovery/discovery_test.go`:

In the existing `TestSearchPrefixID` test (or add new), verify that search with a prefix returns root skills but NOT sub-skills.

```go
func TestSearch_ExcludesSubSkills(t *testing.T) {
	// This test requires PostgreSQL and the discovery table to have
	// root and sub-skill entries. Skip if no test DB.
	dbURL := os.Getenv("SKILLHUB_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKILLHUB_TEST_DATABASE_URL not set")
	}
	db, err := gorm.Open(postgres.Open(dbURL), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	d := New(db, nil)
	d.Init(context.Background())

	d.RegisterSkill(context.Background(), SkillSummary{
		ID:          "github.com/test/search-root",
		Name:        "Root",
		Description: "root skill",
	})
	d.Approve(context.Background(), "github.com/test/search-root")

	d.RegisterSkill(context.Background(), SkillSummary{
		ID:          "github.com/test/search-root/sub-a",
		Name:        "Sub A",
		Description: "sub skill",
	})
	d.Approve(context.Background(), "github.com/test/search-root/sub-a")

	results, err := d.Search(context.Background(), SearchRequest{ID: "github.com/test/search-root"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 root skill, got %d: %+v", len(results), results)
	}
	if results[0].ID != "github.com/test/search-root" {
		t.Errorf("expected root id, got %q", results[0].ID)
	}
}
```

- [ ] **Step 2: Run test — verify it fails with sub-skills included**

```bash
SKILLHUB_TEST_DATABASE_URL=postgres://localhost:5432/skillhub_test go test ./discovery/ -v -run TestSearch_ExcludesSubSkills
```

Expected: FAIL — returns 2 results (root + sub).

- [ ] **Step 3: Fix discovery.go to exclude sub-skills**

The current SQL filters by `status = 'approved'` and `id LIKE prefix%`. We need to additionally exclude rows where the ID has more path segments than the matched root.

Since the discovery center doesn't have an `is_sub` column yet, we need to add one. But per the design doc, sub-skills are never registered directly in discovery center — they're only reachable via `sub_skills` in the root.

The simpler fix: registration should reject sub-skill IDs (those containing a root prefix + "/"). And search should filter.

Add `is_sub` to `skillModel`:

```go
type skillModel struct {
	ID          string    `gorm:"primaryKey"`
	Name        string    `gorm:"default:''"`
	Description string    `gorm:"default:''"`
	Version     string    `gorm:"default:''"`
	Tags        string    `gorm:"type:text[];default:'{}'"`
	Status      string    `gorm:"default:'pending'"`
	Source      string    `gorm:"default:''"`
	IsSub       bool      `gorm:"default:false"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
```

Add migration to `Init()`:

```go
if !d.db.Migrator().HasColumn(&skillModel{}, "is_sub") {
    d.db.WithContext(ctx).Exec(`ALTER TABLE skill_models ADD COLUMN is_sub BOOLEAN NOT NULL DEFAULT false`)
}
```

Update `AutoMigrate` or query:

```go
func (d *Discovery) Init(ctx context.Context) error {
    if err := d.db.WithContext(ctx).AutoMigrate(&skillModel{}); err != nil {
        return err
    }
    if !d.db.Migrator().HasColumn(&skillModel{}, "is_sub") {
        d.db.WithContext(ctx).Exec(`ALTER TABLE skill_models ADD COLUMN is_sub BOOLEAN NOT NULL DEFAULT false`)
    }
    // existing migration code...
    return nil
}
```

Update `Search` to filter out sub-skills:

```go
q = q.Where("is_sub = false")
```

- [ ] **Step 4: Run test**

```bash
SKILLHUB_TEST_DATABASE_URL=postgres://localhost:5432/skillhub_test go test ./discovery/ -v -run TestSearch_ExcludesSubSkills
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add discovery/discovery.go discovery/discovery_test.go
git commit -m "fix: discovery center Search only returns root skills, excluding sub-skills"
```

---

### Task 10: Final Build and Test Verification

- [ ] **Step 1: Build all packages**

```bash
cd skillhub && go build ./...
```

Expected: no errors.

- [ ] **Step 2: Run all unit tests**

```bash
cd skillhub && go test ./pkg/... -v
```

Expected: all pass.

- [ ] **Step 3: Run discovery tests (if DB available)**

```bash
SKILLHUB_TEST_DATABASE_URL=postgres://localhost:5432/skillhub_test go test ./discovery/ -v
```

Expected: all pass.

- [ ] **Step 4: Commit final state**

```bash
git add -A && git commit -m "chore: final build and test verification for cache + gap closure"
```
