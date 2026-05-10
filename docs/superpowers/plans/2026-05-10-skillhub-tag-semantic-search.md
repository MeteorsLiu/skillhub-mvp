# SkillHub Tag Semantic Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `skillhub_search.tag` an indexed broad skill-area search input while keeping `description` as a regex filter for specific intent.

**Architecture:** Discovery owns the authoritative Postgres full-text tag-area index and validation. SkillHub local cache mirrors the public semantics with lightweight token ranking over `tags + name`. MCP and AGENTS guidance describe the parameter split, while a discovery backfill path refreshes existing rows without clearing the database.

**Tech Stack:** Go 1.24/1.25, GORM, Postgres FTS/GIN, SQLite, MCP Go SDK, existing `skillhub fetch` command.

---

## File Structure

- Modify `discovery/discovery.go`: schema migration, tag search vector maintenance, search validation, FTS query, ranking, and backfill methods.
- Modify `discovery/discovery_test.go`: discovery tests for tag semantic search, anti-enumeration validation, ranking, offset, and backfill.
- Modify `discovery/worker.go`: extract reusable fetch/update metadata helper from registration worker.
- Modify `skillhub/pkg/cache/cache.go`: local cache semantic tag search and anti-enumeration validation.
- Modify `skillhub/pkg/cache/cache_test.go`: local search tests.
- Modify `skillhub/pkg/mcp/server.go`: tool descriptions for `tag` and `description`.
- Modify `skillhub/pkg/mcp/server_test.go`: metadata regression tests.
- Modify `doc/Skillhub_Design_Document.md`: API semantics and validation.
- Modify `doc/Skillhub_Agent_Guidance.md`: runtime guidance.
- Modify `/Users/haolan/.config/opencode/AGENTS.md`: local runtime guidance. This file is outside the repo and must not be included in repo commits.

## Task 1: Discovery Validation And FTS Schema

**Files:**
- Modify: `discovery/discovery.go`
- Test: `discovery/discovery_test.go`

- [ ] **Step 1: Write failing tests for validation and default search limits**

Add these tests to `discovery/discovery_test.go`:

```go
func TestSearchRejectsAllMatchDescriptionWithoutTag(t *testing.T) {
	db := connectTestDB(t)
	d := discovery.New(db, nil)
	ctx := context.Background()
	freshTable(t, d, db)

	if err := d.RegisterSkill(ctx, discovery.SkillSummary{ID: "s1", Name: "S1", Description: "anything"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Approve(ctx, "s1"); err != nil {
		t.Fatal(err)
	}

	_, err := d.Search(ctx, discovery.SearchRequest{Description: ".*"})
	if err == nil {
		t.Fatal("expected search to reject all-match description without tag")
	}
}

func TestInitCreatesTagSearchVector(t *testing.T) {
	db := connectTestDB(t)
	d := discovery.New(db, nil)
	ctx := context.Background()
	freshTable(t, d, db)

	var exists bool
	db.WithContext(ctx).Raw(`
		SELECT EXISTS (
			SELECT FROM information_schema.columns
			WHERE table_name = 'skill_models'
			AND column_name = 'tag_search_vector'
		)
	`).Scan(&exists)
	if !exists {
		t.Fatal("tag_search_vector column was not created")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
cd /Users/haolan/project/skills/discovery
go test ./... -run 'TestSearchRejectsAllMatchDescriptionWithoutTag|TestInitCreatesTagSearchVector' -count=1
```

Expected: `TestSearchRejectsAllMatchDescriptionWithoutTag` fails because `.*` is currently accepted, and `TestInitCreatesTagSearchVector` fails because the column does not exist.

- [ ] **Step 3: Implement validation and schema migration**

In `discovery/discovery.go`, add helpers:

```go
func normalizeLimitOffset(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func trimSearchFields(req SearchRequest) SearchRequest {
	req.ID = strings.TrimSpace(req.ID)
	req.Description = strings.TrimSpace(req.Description)
	req.Tag = strings.TrimSpace(req.Tag)
	return req
}

func isAllMatchDescription(description string) bool {
	return strings.TrimSpace(description) == ".*"
}

func validateSearchRequest(req SearchRequest) error {
	if req.ID == "" && req.Description == "" && req.Tag == "" {
		return errors.New("at least one of id, description, or tag must be provided")
	}
	if req.Tag == "" && isAllMatchDescription(req.Description) {
		return errors.New("description all-match regex requires tag")
	}
	return nil
}
```

Add `errors` to imports.

Extend `skillModel`:

```go
TagSearchVector string `gorm:"type:tsvector"`
```

Update `Init` to add column and index explicitly:

```go
if !d.db.Migrator().HasColumn(&skillModel{}, "tag_search_vector") {
	if err := d.db.WithContext(ctx).Exec(`ALTER TABLE skill_models ADD COLUMN tag_search_vector tsvector`).Error; err != nil {
		return err
	}
}
if err := d.db.WithContext(ctx).Exec(`
	CREATE INDEX IF NOT EXISTS idx_skill_models_tag_search_vector
	ON skill_models USING GIN (tag_search_vector)
`).Error; err != nil {
	return err
}
```

At the start of `Search`, normalize and validate:

```go
req = trimSearchFields(req)
if err := validateSearchRequest(req); err != nil {
	return nil, err
}
limit, offset := normalizeLimitOffset(req.Limit, req.Offset)
```

Remove duplicate inline limit/offset normalization from `Search`.

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
cd /Users/haolan/project/skills/discovery
go test ./... -run 'TestSearchRejectsAllMatchDescriptionWithoutTag|TestInitCreatesTagSearchVector' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add discovery/discovery.go discovery/discovery_test.go
git commit -m "Add discovery search validation and tag index schema"
```

## Task 2: Discovery Tag Semantic Search

**Files:**
- Modify: `discovery/discovery.go`
- Test: `discovery/discovery_test.go`

- [ ] **Step 1: Write failing semantic tag tests**

Add these tests to `discovery/discovery_test.go`:

```go
func TestSearchBySemanticTagUsesTagsAndName(t *testing.T) {
	db := connectTestDB(t)
	d := discovery.New(db, nil)
	ctx := context.Background()
	freshTable(t, d, db)

	skills := []discovery.SkillSummary{
		{ID: "finance-stock", Name: "Stock Market Lookup", Tags: []string{"finance", "market"}},
		{ID: "persona-jobs", Name: "Steve Jobs Persona", Tags: []string{"persona", "style"}},
	}
	for _, s := range skills {
		if err := d.RegisterSkill(ctx, s); err != nil {
			t.Fatal(err)
		}
		if err := d.Approve(ctx, s.ID); err != nil {
			t.Fatal(err)
		}
	}

	results, err := d.Search(ctx, discovery.SearchRequest{Tag: "financial market"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	if results[0].ID != "finance-stock" {
		t.Fatalf("expected finance-stock, got %q", results[0].ID)
	}
}

func TestSearchTagIsNotRegex(t *testing.T) {
	db := connectTestDB(t)
	d := discovery.New(db, nil)
	ctx := context.Background()
	freshTable(t, d, db)

	for _, s := range []discovery.SkillSummary{
		{ID: "finance-stock", Name: "Stock Lookup", Tags: []string{"finance"}},
		{ID: "weather-current", Name: "Weather Lookup", Tags: []string{"weather"}},
	} {
		if err := d.RegisterSkill(ctx, s); err != nil {
			t.Fatal(err)
		}
		if err := d.Approve(ctx, s.ID); err != nil {
			t.Fatal(err)
		}
	}

	results, err := d.Search(ctx, discovery.SearchRequest{Tag: ".*"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected regex-like tag not to enumerate rows, got %+v", results)
	}
}

func TestSearchTagAllowsAllMatchDescriptionWithinArea(t *testing.T) {
	db := connectTestDB(t)
	d := discovery.New(db, nil)
	ctx := context.Background()
	freshTable(t, d, db)

	for _, s := range []discovery.SkillSummary{
		{ID: "persona-jobs", Name: "Steve Jobs Persona", Description: "style", Tags: []string{"persona"}},
		{ID: "finance-stock", Name: "Stock Lookup", Description: "price", Tags: []string{"finance"}},
	} {
		if err := d.RegisterSkill(ctx, s); err != nil {
			t.Fatal(err)
		}
		if err := d.Approve(ctx, s.ID); err != nil {
			t.Fatal(err)
		}
	}

	results, err := d.Search(ctx, discovery.SearchRequest{Tag: "persona", Description: ".*"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != "persona-jobs" {
		t.Fatalf("expected only persona-jobs, got %+v", results)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
cd /Users/haolan/project/skills/discovery
go test ./... -run 'TestSearchBySemanticTagUsesTagsAndName|TestSearchTagIsNotRegex|TestSearchTagAllowsAllMatchDescriptionWithinArea' -count=1
```

Expected: FAIL because tag still uses regex and no FTS vector is populated.

- [ ] **Step 3: Populate tag search vector on register**

In `discovery/discovery.go`, add helper:

```go
func tagSearchText(skill SkillSummary) string {
	parts := make([]string, 0, len(skill.Tags)+1)
	parts = append(parts, skill.Tags...)
	if skill.Name != "" {
		parts = append(parts, skill.Name)
	}
	return strings.Join(parts, " ")
}
```

Replace `RegisterSkill` save with a transaction or follow-up update that updates the vector:

```go
func (d *Discovery) RegisterSkill(ctx context.Context, skill SkillSummary) error {
	m := skillModel{
		ID:          skill.ID,
		Name:        skill.Name,
		Description: skill.Description,
		Version:     skill.Version,
		Tags:        joinTags(skill.Tags),
		Status:      "pending",
	}
	if err := d.db.WithContext(ctx).Save(&m).Error; err != nil {
		return err
	}
	return d.updateTagSearchVector(ctx, skill.ID, skill)
}

func (d *Discovery) updateTagSearchVector(ctx context.Context, id string, skill SkillSummary) error {
	return d.db.WithContext(ctx).Exec(`
		UPDATE skill_models
		SET tag_search_vector =
			setweight(to_tsvector('english', coalesce(?, '')), 'A') ||
			setweight(to_tsvector('english', coalesce(?, '')), 'B')
		WHERE id = ?
	`, strings.Join(skill.Tags, " "), skill.Name, id).Error
}
```

- [ ] **Step 4: Replace tag regex filtering with FTS filtering and ranking**

In `Search`, replace:

```go
if req.Tag != "" {
	q = q.Where("EXISTS (SELECT 1 FROM unnest(tags) t WHERE t ~* ?)", req.Tag)
}
```

with:

```go
tagQuery := ""
if req.Tag != "" {
	tagQuery = req.Tag
	q = q.Where("tag_search_vector @@ plainto_tsquery('english', ?)", tagQuery)
}
```

Do not interpolate `tagQuery` into SQL strings. Use GORM's parameter binding for the tag-rank order expression:

```go
if tagQuery != "" {
	q = q.Order(clause.Expr{
		SQL:  "ts_rank_cd(tag_search_vector, plainto_tsquery('english', ?)) DESC",
		Vars: []any{tagQuery},
	})
}
q = q.Order("created_at DESC, id ASC")
```

Add `gorm.io/gorm/clause` to imports.

- [ ] **Step 5: Keep description ranking from overriding tag ranking**

In `Search`, only call `rankModels(models, searchTokens(req.Description))` when `req.Tag == ""`.

```go
if req.Tag == "" {
	rankModels(models, searchTokens(req.Description))
}
```

This preserves FTS rank order when tag is used.

- [ ] **Step 6: Run tests to verify they pass**

Run:

```bash
cd /Users/haolan/project/skills/discovery
go test ./... -run 'TestSearchBySemanticTagUsesTagsAndName|TestSearchTagIsNotRegex|TestSearchTagAllowsAllMatchDescriptionWithinArea' -count=1
```

Expected: PASS.

- [ ] **Step 7: Run broader discovery tests**

Run:

```bash
cd /Users/haolan/project/skills/discovery
go test ./... -count=1
```

Expected: PASS, or DB-dependent tests skip if `SKILLHUB_TEST_DATABASE_URL` is not set.

- [ ] **Step 8: Commit**

```bash
git add discovery/discovery.go discovery/discovery_test.go
git commit -m "Add semantic tag search to discovery"
```

## Task 3: Local Cache Tag Semantics

**Files:**
- Modify: `skillhub/pkg/cache/cache.go`
- Test: `skillhub/pkg/cache/cache_test.go`

- [ ] **Step 1: Write failing local cache tests**

Add tests to `skillhub/pkg/cache/cache_test.go`:

```go
func TestSearch_TagSemanticMatch(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	skillsRoot := filepath.Join(dir, "skills")

	c, err := cache.Open(dbPath, skillsRoot)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer c.Close()

	c.Upsert(types.SkillSummary{ID: "finance", Name: "Stock Market Lookup", Tags: []string{"finance", "market"}}, "local")
	c.Upsert(types.SkillSummary{ID: "weather", Name: "Weather Lookup", Tags: []string{"weather"}}, "local")

	results, err := c.Search("", "financial market", 10, 0)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].ID != "finance" {
		t.Fatalf("expected finance result, got %+v", results)
	}
}

func TestSearch_TagIsNotRegex(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	skillsRoot := filepath.Join(dir, "skills")

	c, err := cache.Open(dbPath, skillsRoot)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer c.Close()

	c.Upsert(types.SkillSummary{ID: "finance", Name: "Stock Lookup", Tags: []string{"finance"}}, "local")
	c.Upsert(types.SkillSummary{ID: "weather", Name: "Weather Lookup", Tags: []string{"weather"}}, "local")

	results, err := c.Search("", ".*", 10, 0)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected regex tag not to enumerate rows, got %+v", results)
	}
}

func TestSearch_AllMatchDescriptionRequiresTag(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	skillsRoot := filepath.Join(dir, "skills")

	c, err := cache.Open(dbPath, skillsRoot)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer c.Close()

	_, err = c.Search(".*", "", 10, 0)
	if err == nil {
		t.Fatal("expected all-match description without tag to fail")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
cd /Users/haolan/project/skills/skillhub
go test ./pkg/cache -run 'TestSearch_TagSemanticMatch|TestSearch_TagIsNotRegex|TestSearch_AllMatchDescriptionRequiresTag' -count=1
```

Expected: FAIL because tag still uses LIKE/regex-like broad matching and `description=.*` is accepted.

- [ ] **Step 3: Add local validation and token ranking**

In `skillhub/pkg/cache/cache.go`, add:

```go
func validateSearch(description, tag string) error {
	if strings.TrimSpace(description) == "" && strings.TrimSpace(tag) == "" {
		return nil
	}
	if strings.TrimSpace(tag) == "" && strings.TrimSpace(description) == ".*" {
		return fmt.Errorf("description all-match regex requires tag")
	}
	return nil
}

func tagTokens(tag string) []string {
	return searchTokens(strings.ToLower(tag))
}

func tagScore(summary types.SkillSummary, tokens []string) int {
	if len(tokens) == 0 {
		return 0
	}
	tagText := strings.ToLower(strings.Join(summary.Tags, " "))
	nameText := strings.ToLower(summary.Name)
	score := 0
	for _, token := range tokens {
		if strings.Contains(tagText, token) {
			score += 3
		}
		if strings.Contains(nameText, token) {
			score += 1
		}
	}
	return score
}
```

At the start of `Search`, trim and validate `description` and `tag`.

- [ ] **Step 4: Replace tag LIKE search with candidate scan and tag scoring**

In `Search`, stop using `tag` as `likePattern`. Query all rows ordered by `id` when only tag is present, then filter in memory:

```go
tagParts := tagTokens(tag)
...
if tag != "" {
	var filtered []types.SkillSummary
	for _, s := range result {
		if tagScore(s, tagParts) > 0 {
			filtered = append(filtered, s)
		}
	}
	result = filtered
	sort.SliceStable(result, func(i, j int) bool {
		left := tagScore(result[i], tagParts)
		right := tagScore(result[j], tagParts)
		if left != right {
			return left > right
		}
		return result[i].ID < result[j].ID
	})
} else {
	rankSummaries(result, searchTokens(description))
}
```

Keep description regex filtering after candidate load. `description=.*` is allowed when `tag != ""`.

- [ ] **Step 5: Run local cache tests**

Run:

```bash
cd /Users/haolan/project/skills/skillhub
go test ./pkg/cache -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add skillhub/pkg/cache/cache.go skillhub/pkg/cache/cache_test.go
git commit -m "Make local cache tag search semantic"
```

## Task 4: MCP And Guidance Text

**Files:**
- Modify: `skillhub/pkg/mcp/server.go`
- Modify: `skillhub/pkg/mcp/server_test.go`
- Modify: `doc/Skillhub_Agent_Guidance.md`
- Modify: `doc/Skillhub_Design_Document.md`
- Modify: `/Users/haolan/.config/opencode/AGENTS.md`

- [ ] **Step 1: Write failing MCP metadata test**

Add to `skillhub/pkg/mcp/server_test.go`:

```go
func TestSearchToolDescribesTagAndDescriptionSemantics(t *testing.T) {
	srv := skillhubmcp.NewServer(&mockTools{})
	tools := listTools(t, srv)

	var search *mcp.Tool
	for i := range tools {
		if tools[i].Name == "skillhub_search" {
			search = &tools[i]
			break
		}
	}
	if search == nil {
		t.Fatal("missing skillhub_search")
	}

	data, err := json.Marshal(search)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"Use tag for the broad skill area, and description for the specific user intent",
		"English broad skill area hint. Not regex",
		"Regex pattern for the specific user intent",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("tool metadata missing %q in %s", want, text)
		}
	}
}
```

Add `strings` to imports.

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
cd /Users/haolan/project/skills/skillhub
go test ./pkg/mcp -run TestSearchToolDescribesTagAndDescriptionSemantics -count=1
```

Expected: FAIL because metadata still says tag is regex and description is an English query.

- [ ] **Step 3: Update MCP descriptions**

In `skillhub/pkg/mcp/server.go`, change:

```go
"Search in English for the user's actual intent, not just keywords.\n"+
```

to:

```go
"Search in English. Use tag for the broad skill area, and description for the specific user intent.\n"+
```

Change parameter descriptions:

```go
mcp.WithString("description", mcp.Description("Regex pattern for the specific user intent, matched against skill name and description")),
mcp.WithString("tag", mcp.Description("English broad skill area hint. Not regex")),
```

- [ ] **Step 4: Update guidance docs**

In `doc/Skillhub_Agent_Guidance.md` and `/Users/haolan/.config/opencode/AGENTS.md`, replace:

```text
Search in English for the user's actual intent, not just keywords.
```

with:

```text
Search in English. Use tag for the broad skill area, and description for the specific user intent.
```

In `doc/Skillhub_Design_Document.md`, change the `skillhub_search` bullets:

```md
- `description` 按正则匹配 skill name 和 description
- `tag` 是英文 broad skill area hint，不是正则
- `tag` 用 Postgres FTS / 本地 token ranking 做语义化领域召回
```

- [ ] **Step 5: Run MCP tests**

Run:

```bash
cd /Users/haolan/project/skills/skillhub
go test ./pkg/mcp -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add skillhub/pkg/mcp/server.go skillhub/pkg/mcp/server_test.go doc/Skillhub_Agent_Guidance.md doc/Skillhub_Design_Document.md
git commit -m "Clarify SkillHub tag search guidance"
```

Do not attempt to commit `/Users/haolan/.config/opencode/AGENTS.md`; it is outside the repository. Mention the external local guidance update in the final report.

## Task 5: Metadata Backfill Path

**Files:**
- Modify: `discovery/worker.go`
- Modify: `discovery/discovery.go`
- Modify: `discovery/server.go`
- Test: `discovery/discovery_test.go`

- [ ] **Step 1: Write failing backfill unit test**

Add to `discovery/discovery_test.go`:

```go
func TestBackfillSkillMetadataPreservesApproval(t *testing.T) {
	db := connectTestDB(t)
	d := discovery.New(db, nil)
	ctx := context.Background()
	freshTable(t, d, db)

	if err := d.RegisterSkill(ctx, discovery.SkillSummary{ID: "s1", Name: "Old", Description: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Approve(ctx, "s1"); err != nil {
		t.Fatal(err)
	}

	err := d.BackfillSkillMetadata(ctx, discovery.SkillSummary{
		ID:          "s1",
		Name:        "Stock Lookup",
		Description: "Lookup stock prices",
		Version:     "2.0",
		Tags:        []string{"finance", "market"},
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := d.Search(ctx, discovery.SearchRequest{Tag: "finance", Description: ".*"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != "s1" {
		t.Fatalf("expected backfilled approved skill, got %+v", results)
	}
	if results[0].Name != "Stock Lookup" || results[0].Version != "2.0" {
		t.Fatalf("metadata not updated: %+v", results[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
cd /Users/haolan/project/skills/discovery
go test ./... -run TestBackfillSkillMetadataPreservesApproval -count=1
```

Expected: FAIL because `BackfillSkillMetadata` does not exist.

- [ ] **Step 3: Implement metadata backfill method**

In `discovery/discovery.go`, add:

```go
func (d *Discovery) BackfillSkillMetadata(ctx context.Context, skill SkillSummary) error {
	if strings.TrimSpace(skill.ID) == "" {
		return errors.New("id is required")
	}
	var existing skillModel
	if err := d.db.WithContext(ctx).Where("id = ?", skill.ID).First(&existing).Error; err != nil {
		return err
	}
	updates := map[string]any{
		"name":        skill.Name,
		"description": skill.Description,
		"version":     skill.Version,
		"tags":        joinTags(skill.Tags),
	}
	if err := d.db.WithContext(ctx).Model(&skillModel{}).Where("id = ?", skill.ID).Updates(updates).Error; err != nil {
		return err
	}
	return d.updateTagSearchVector(ctx, skill.ID, skill)
}
```

This intentionally does not update `status`, so approval state is preserved.

- [ ] **Step 4: Extract reusable fetch metadata helper**

In `discovery/worker.go`, extract the fetch logic:

```go
func FetchSkillMetadata(id, version string) (SkillSummary, string, error) {
	tmpDir, err := os.MkdirTemp("", "discovery-worker-*")
	if err != nil {
		return SkillSummary{}, "", err
	}
	cleanup := true
	defer func() {
		if cleanup {
			os.RemoveAll(tmpDir)
		}
	}()

	if version == "" {
		version = "latest"
	}
	cmd := exec.Command("skillhub", "fetch", id+"@"+version, tmpDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return SkillSummary{}, "", fmt.Errorf("fetch %s failed: %s", id, string(out))
	}
	var skill SkillSummary
	if err := json.Unmarshal(out, &skill); err != nil {
		return SkillSummary{}, "", err
	}
	cleanup = false
	return skill, tmpDir, nil
}
```

Add `fmt` to imports. Update `HandleRegisterSkill` to call this helper and `defer os.RemoveAll(tmpDir)` after successful fetch.

- [ ] **Step 5: Run backfill tests**

Run:

```bash
cd /Users/haolan/project/skills/discovery
go test ./... -run 'TestBackfillSkillMetadataPreservesApproval|TestSearchBySemanticTagUsesTagsAndName' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add discovery/discovery.go discovery/discovery_test.go discovery/worker.go
git commit -m "Add discovery metadata backfill"
```

## Task 6: Full Verification

**Files:**
- All touched files.

- [ ] **Step 1: Format Go files**

Run:

```bash
cd /Users/haolan/project/skills
gofmt -w discovery/discovery.go discovery/discovery_test.go discovery/server.go discovery/worker.go skillhub/pkg/cache/cache.go skillhub/pkg/cache/cache_test.go skillhub/pkg/mcp/server.go skillhub/pkg/mcp/server_test.go
```

Expected: no output.

- [ ] **Step 2: Run SkillHub tests**

Run:

```bash
cd /Users/haolan/project/skills/skillhub
go test ./...
```

Expected: PASS.

- [ ] **Step 3: Build SkillHub**

Run:

```bash
cd /Users/haolan/project/skills/skillhub
go build ./...
```

Expected: PASS.

- [ ] **Step 4: Run discovery tests**

Run:

```bash
cd /Users/haolan/project/skills/discovery
go test ./...
```

Expected: PASS, or DB-dependent tests skip if `SKILLHUB_TEST_DATABASE_URL` is unset.

- [ ] **Step 5: Build discovery**

Run:

```bash
cd /Users/haolan/project/skills/discovery
go build ./...
```

Expected: PASS.

- [ ] **Step 6: Commit verification fixes if any**

If formatting or fixes changed files:

```bash
git add discovery skillhub doc
git commit -m "Verify SkillHub tag semantic search"
```

If only external AGENTS changed, do not commit anything; mention it in the final report.
