# SkillHub Promoted Search Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add promoted local search caching for SkillHub MCP search using query observations, SQLite FTS5/BM25, and result-stability promotion.

**Architecture:** MCP remains the owner of search/load cache data. The existing `cache.Search` local skill lookup stays in place for dependency/name lookup, while new promoted search-cache APIs handle remote-search observations and 24h promoted result reuse. Discovery remains the freshness source: first searches only record observations, and promotion still calls discovery once before writing a cache entry.

**Tech Stack:** Go 1.25, `modernc.org/sqlite` FTS5/BM25, `github.com/go-ego/gse`, embedded jieba official dictionary, existing `skillhub/pkg/types`.

---

### File Structure

- Create: `skillhub/pkg/cache/tokenizer.go`
  - Owns gse initialization, embedded jieba dictionary loading, ASCII token preservation, and FTS query escaping.
- Create: `skillhub/pkg/cache/jieba.dict.txt`
  - Embedded jieba official dictionary used by tokenizer.
- Create: `skillhub/pkg/cache/search_cache.go`
  - Owns observation schema, promoted result schema, BM25 observation lookup, weighted result-stability scoring, promoted cache read/write APIs, and cleanup.
- Modify: `skillhub/pkg/cache/cache.go`
  - Initialize promoted search-cache schema/tokenizer from `Open`.
- Modify: `skillhub/pkg/cache/cache_test.go`
  - Add unit tests for tokenization, observation writes, no first-search promotion, 3-observation promotion, split-result non-promotion, 24h expiry, and Chinese/ASCII matching.
- Modify: `skillhub/cmd/skillhub/main.go`
  - Wire `mcpCore.Search` through promoted cache APIs while preserving remote discovery fallback.
- Modify: `skillhub/cmd/skillhub/main_test.go`
  - Replace the old "bypasses local cache" assertion with promoted-cache behavior tests.
- Modify: `skillhub/go.mod`, `skillhub/go.sum`
  - Add `github.com/go-ego/gse`.

### Task 1: Add Tokenizer

**Files:**
- Create: `skillhub/pkg/cache/tokenizer.go`
- Create: `skillhub/pkg/cache/jieba.dict.txt`
- Modify: `skillhub/go.mod`
- Test: `skillhub/pkg/cache/cache_test.go`

- [ ] **Step 1: Copy jieba official dictionary**

Use the locally verified jieba dictionary:

```bash
cp /Users/haolan/.local/lib/python3.13/site-packages/jieba/dict.txt skillhub/pkg/cache/jieba.dict.txt
```

- [ ] **Step 2: Add gse dependency**

Run:

```bash
cd skillhub
go get github.com/go-ego/gse@v1.0.2
```

Expected: `skillhub/go.mod` contains `github.com/go-ego/gse v1.0.2`.

- [ ] **Step 3: Write failing tokenizer test**

Append to `skillhub/pkg/cache/cache_test.go`:

```go
func TestTokenizer_ChineseAndASCIITokens(t *testing.T) {
	tok, err := cache.NewTokenizer()
	if err != nil {
		t.Fatalf("NewTokenizer: %v", err)
	}

	tokens := tok.Tokens("小红书浦东明珠租房补贴 pgvector API")
	got := map[string]bool{}
	for _, token := range tokens {
		got[token] = true
	}

	for _, want := range []string{"小红书", "浦东", "明珠", "租房", "补贴", "pgvector", "api"} {
		if !got[want] {
			t.Fatalf("missing token %q from %#v", want, tokens)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run:

```bash
cd skillhub
go test ./pkg/cache -run TestTokenizer_ChineseAndASCIITokens -count=1
```

Expected: FAIL because `cache.NewTokenizer` does not exist.

- [ ] **Step 5: Implement tokenizer**

Create `skillhub/pkg/cache/tokenizer.go`:

```go
package cache

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/go-ego/gse"
)

//go:embed jieba.dict.txt
var jiebaDict string

var asciiTokenRE = regexp.MustCompile(`[A-Za-z0-9_+#.-]+`)

type Tokenizer struct {
	seg gse.Segmenter
}

func NewTokenizer() (*Tokenizer, error) {
	var tok Tokenizer
	if err := tok.seg.LoadDictEmbed(jiebaDict); err != nil {
		return nil, err
	}
	return &tok, nil
}

func (t *Tokenizer) Tokens(text string) []string {
	seen := map[string]bool{}
	tokens := make([]string, 0, 16)
	add := func(token string) {
		token = strings.TrimSpace(strings.ToLower(token))
		if len([]rune(token)) < 2 || seen[token] {
			return
		}
		seen[token] = true
		tokens = append(tokens, token)
	}
	for _, token := range t.seg.CutSearch(text, true) {
		add(token)
	}
	for _, token := range asciiTokenRE.FindAllString(text, -1) {
		add(token)
	}
	return tokens
}

func ftsMatchExpr(tokens []string) string {
	if len(tokens) > 24 {
		tokens = tokens[:24]
	}
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		encoded, _ := json.Marshal(token)
		parts = append(parts, string(encoded))
	}
	return strings.Join(parts, " OR ")
}
```

- [ ] **Step 6: Run tokenizer test**

Run:

```bash
cd skillhub
go test ./pkg/cache -run TestTokenizer_ChineseAndASCIITokens -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit tokenizer**

```bash
git add skillhub/go.mod skillhub/go.sum skillhub/pkg/cache/tokenizer.go skillhub/pkg/cache/jieba.dict.txt skillhub/pkg/cache/cache_test.go
git commit -m "feat: add skillhub search tokenizer"
```

### Task 2: Add Promoted Search Cache Storage And Scoring

**Files:**
- Create: `skillhub/pkg/cache/search_cache.go`
- Modify: `skillhub/pkg/cache/cache.go`
- Modify: `skillhub/pkg/cache/cache_test.go`

- [ ] **Step 1: Write failing no-first-promotion test**

Append to `skillhub/pkg/cache/cache_test.go`:

```go
func TestPromotedSearchCache_DoesNotPromoteFirstObservation(t *testing.T) {
	c := openTestCache(t)
	defer c.Close()

	req := types.SearchRequest{Description: "小红书浦东明珠租房补贴", Tag: "xiaohongshu", Limit: 10}
	results := []types.SkillSummary{{ID: "xiaohongshu-browser", Name: "XHS Browser", Description: "research xiaohongshu"}}

	if hit, err := c.GetPromotedSearch(req); err != nil {
		t.Fatalf("GetPromotedSearch: %v", err)
	} else if hit != nil {
		t.Fatalf("expected no promoted hit before observation, got %+v", hit)
	}

	if err := c.RecordSearchObservation(req, results); err != nil {
		t.Fatalf("RecordSearchObservation: %v", err)
	}

	if ok, err := c.ShouldPromoteSearch(req); err != nil {
		t.Fatalf("ShouldPromoteSearch: %v", err)
	} else if ok {
		t.Fatal("first observation should not promote")
	}
}
```

Also add helper:

```go
func openTestCache(t *testing.T) *cache.Cache {
	t.Helper()
	dir := t.TempDir()
	c, err := cache.Open(filepath.Join(dir, "test.db"), filepath.Join(dir, "skills"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	return c
}
```

Replace duplicated open blocks in new tests only; existing tests can stay as-is.

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
cd skillhub
go test ./pkg/cache -run TestPromotedSearchCache_DoesNotPromoteFirstObservation -count=1
```

Expected: FAIL because promoted cache methods do not exist.

- [ ] **Step 3: Initialize schema from `Open`**

Modify `skillhub/pkg/cache/cache.go`:

```go
type Cache struct {
	db        *sql.DB
	tokenizer *Tokenizer
}
```

In `Open`, after creating the existing `skills` table:

```go
	tok, err := NewTokenizer()
	if err != nil {
		return nil, fmt.Errorf("init tokenizer: %w", err)
	}
	c := &Cache{db: db, tokenizer: tok}
	if err := c.initSearchCacheSchema(); err != nil {
		return nil, fmt.Errorf("init search cache schema: %w", err)
	}
```

Keep the existing `syncFromFS(skillsRoot)` call after schema initialization.

- [ ] **Step 4: Implement schema and public APIs**

Create `skillhub/pkg/cache/search_cache.go`:

```go
package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"skillhub/pkg/types"
)

const promotedSearchTTL = 24 * time.Hour

type searchObservationResult struct {
	ID   string `json:"id"`
	Rank int    `json:"rank"`
}

type searchObservation struct {
	ID      int64
	Score   float64
	Results []searchObservationResult
}

type resultStats struct {
	id          string
	score       float64
	appearances int
	rankSum     int
}

func (c *Cache) initSearchCacheSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS search_observations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			query_text TEXT NOT NULL,
			query_tokens TEXT NOT NULL,
			results_json TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS search_observations_fts USING fts5(
			query_tokens,
			content='search_observations',
			content_rowid='id'
		)`,
		`CREATE TABLE IF NOT EXISTS promoted_search_cache (
			cache_key TEXT PRIMARY KEY,
			query_tokens TEXT NOT NULL,
			results_json TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			expires_at DATETIME NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := c.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cache) GetPromotedSearch(req types.SearchRequest) ([]types.SkillSummary, error) {
	key := c.promotedSearchKey(req)
	var raw string
	var expiresAt time.Time
	err := c.db.QueryRow(`SELECT results_json, expires_at FROM promoted_search_cache WHERE cache_key = ?`, key).Scan(&raw, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !time.Now().UTC().Before(expiresAt) {
		_, _ = c.db.Exec(`DELETE FROM promoted_search_cache WHERE cache_key = ?`, key)
		return nil, nil
	}
	var results []types.SkillSummary
	if err := json.Unmarshal([]byte(raw), &results); err != nil {
		return nil, err
	}
	return applyLimitOffset(results, req.Limit, req.Offset), nil
}

func (c *Cache) PutPromotedSearch(req types.SearchRequest, results []types.SkillSummary) error {
	key := c.promotedSearchKey(req)
	tokens := strings.Join(c.searchTokens(req), " ")
	raw, err := json.Marshal(results)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = c.db.Exec(
		`INSERT INTO promoted_search_cache (cache_key, query_tokens, results_json, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(cache_key) DO UPDATE SET
		   query_tokens = excluded.query_tokens,
		   results_json = excluded.results_json,
		   created_at = excluded.created_at,
		   expires_at = excluded.expires_at`,
		key, tokens, string(raw), now, now.Add(promotedSearchTTL),
	)
	return err
}

func (c *Cache) RecordSearchObservation(req types.SearchRequest, results []types.SkillSummary) error {
	if len(results) == 0 {
		return nil
	}
	tokens := c.searchTokens(req)
	if len(tokens) == 0 {
		return nil
	}
	obsResults := make([]searchObservationResult, 0, minInt(len(results), 5))
	for i, result := range results {
		if i >= 5 {
			break
		}
		obsResults = append(obsResults, searchObservationResult{ID: result.ID, Rank: i + 1})
	}
	raw, err := json.Marshal(obsResults)
	if err != nil {
		return err
	}
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(
		`INSERT INTO search_observations (query_text, query_tokens, results_json, created_at) VALUES (?, ?, ?, ?)`,
		c.searchText(req), strings.Join(tokens, " "), string(raw), time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	rowID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO search_observations_fts(rowid, query_tokens) VALUES (?, ?)`, rowID, strings.Join(tokens, " ")); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Cache) ShouldPromoteSearch(req types.SearchRequest) (bool, error) {
	observations, err := c.similarObservations(req, 20)
	if err != nil {
		return false, err
	}
	return stableObservationResults(observations), nil
}

func (c *Cache) similarObservations(req types.SearchRequest, limit int) ([]searchObservation, error) {
	tokens := c.searchTokens(req)
	if len(tokens) == 0 {
		return nil, nil
	}
	expr := ftsMatchExpr(tokens)
	rows, err := c.db.Query(
		`SELECT o.id, bm25(search_observations_fts) AS score, o.results_json
		 FROM search_observations_fts
		 JOIN search_observations o ON o.id = search_observations_fts.rowid
		 WHERE search_observations_fts MATCH ?
		 ORDER BY score
		 LIMIT ?`,
		expr, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []searchObservation
	for rows.Next() {
		var obs searchObservation
		var raw string
		if err := rows.Scan(&obs.ID, &obs.Score, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &obs.Results); err != nil {
			return nil, err
		}
		out = append(out, obs)
	}
	return out, rows.Err()
}

func stableObservationResults(observations []searchObservation) bool {
	if len(observations) < 3 {
		return false
	}
	statsByID := map[string]*resultStats{}
	total := 0.0
	for _, obs := range observations {
		for _, result := range obs.Results {
			weight := rankWeight(result.Rank)
			if weight == 0 {
				continue
			}
			stats := statsByID[result.ID]
			if stats == nil {
				stats = &resultStats{id: result.ID}
				statsByID[result.ID] = stats
			}
			stats.score += float64(weight)
			stats.appearances++
			stats.rankSum += result.Rank
			total += float64(weight)
		}
	}
	if total == 0 {
		return false
	}
	stats := make([]*resultStats, 0, len(statsByID))
	for _, stat := range statsByID {
		stats = append(stats, stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].score != stats[j].score {
			return stats[i].score > stats[j].score
		}
		return stats[i].id < stats[j].id
	})
	best := stats[0]
	avgRank := float64(best.rankSum) / float64(best.appearances)
	return best.appearances >= 2 && avgRank <= 2 && best.score/total >= 0.40
}

func rankWeight(rank int) int {
	switch rank {
	case 1:
		return 5
	case 2:
		return 4
	case 3:
		return 3
	case 4:
		return 2
	case 5:
		return 1
	default:
		return 0
	}
}

func (c *Cache) searchTokens(req types.SearchRequest) []string {
	return c.tokenizer.Tokens(c.searchText(req))
}

func (c *Cache) searchText(req types.SearchRequest) string {
	return strings.TrimSpace(req.ID + " " + req.Tag + " " + req.Description)
}

func (c *Cache) promotedSearchKey(req types.SearchRequest) string {
	return strings.Join(c.searchTokens(req), " ")
}

func applyLimitOffset(results []types.SkillSummary, limit, offset int) []types.SkillSummary {
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(results) {
		return []types.SkillSummary{}
	}
	end := offset + limit
	if end > len(results) {
		end = len(results)
	}
	out := append([]types.SkillSummary(nil), results[offset:end]...)
	for i := range out {
		resultOffset := offset + i
		out[i].Offset = &resultOffset
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

- [ ] **Step 5: Run no-first-promotion test**

Run:

```bash
cd skillhub
go test ./pkg/cache -run TestPromotedSearchCache_DoesNotPromoteFirstObservation -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit storage foundation**

```bash
git add skillhub/pkg/cache/cache.go skillhub/pkg/cache/search_cache.go skillhub/pkg/cache/cache_test.go
git commit -m "feat: add promoted search cache storage"
```

### Task 3: Add Promotion Behavior Tests

**Files:**
- Modify: `skillhub/pkg/cache/cache_test.go`
- Modify: `skillhub/pkg/cache/search_cache.go`

- [ ] **Step 1: Add stable-promotion test**

Append:

```go
func TestPromotedSearchCache_PromotesAfterThreeStableObservations(t *testing.T) {
	c := openTestCache(t)
	defer c.Close()

	reqs := []types.SearchRequest{
		{Description: "小红书浦东明珠租房补贴"},
		{Description: "浦东小红书租房补贴调研"},
		{Description: "小红书 明珠 租房 补贴"},
	}
	resultSets := [][]types.SkillSummary{
		{{ID: "xiaohongshu-browser"}, {ID: "policy-helper"}, {ID: "web-research"}},
		{{ID: "xiaohongshu-browser"}, {ID: "web-research"}, {ID: "policy-helper"}},
		{{ID: "policy-helper"}, {ID: "xiaohongshu-browser"}, {ID: "web-research"}},
	}

	for i := range reqs {
		if err := c.RecordSearchObservation(reqs[i], resultSets[i]); err != nil {
			t.Fatalf("record observation %d: %v", i, err)
		}
	}

	ok, err := c.ShouldPromoteSearch(types.SearchRequest{Description: "帮我调研小红书浦东租房补贴"})
	if err != nil {
		t.Fatalf("ShouldPromoteSearch: %v", err)
	}
	if !ok {
		t.Fatal("expected stable observations to promote")
	}
}
```

- [ ] **Step 2: Add split-result non-promotion test**

Append:

```go
func TestPromotedSearchCache_DoesNotPromoteSplitResults(t *testing.T) {
	c := openTestCache(t)
	defer c.Close()

	reqs := []types.SearchRequest{
		{Description: "小红书浦东租房补贴"},
		{Description: "小红书爆款笔记写作"},
		{Description: "小红书账号运营"},
	}
	resultSets := [][]types.SkillSummary{
		{{ID: "housing-policy"}, {ID: "web-research"}, {ID: "xiaohongshu-browser"}},
		{{ID: "content-writing"}, {ID: "copywriting"}, {ID: "xiaohongshu-browser"}},
		{{ID: "account-growth"}, {ID: "social-ops"}, {ID: "xiaohongshu-browser"}},
	}

	for i := range reqs {
		if err := c.RecordSearchObservation(reqs[i], resultSets[i]); err != nil {
			t.Fatalf("record observation %d: %v", i, err)
		}
	}

	ok, err := c.ShouldPromoteSearch(types.SearchRequest{Description: "小红书浦东明珠租房补贴"})
	if err != nil {
		t.Fatalf("ShouldPromoteSearch: %v", err)
	}
	if ok {
		t.Fatal("split remote results should not promote")
	}
}
```

- [ ] **Step 3: Add promoted cache TTL test**

Append:

```go
func TestPromotedSearchCache_ReturnsWithinTTL(t *testing.T) {
	c := openTestCache(t)
	defer c.Close()

	req := types.SearchRequest{Description: "股票查询 A股 港股 美股 实时行情", Limit: 1}
	results := []types.SkillSummary{
		{ID: "stock-lookup", Name: "Stock Lookup"},
		{ID: "market-news", Name: "Market News"},
	}
	if err := c.PutPromotedSearch(req, results); err != nil {
		t.Fatalf("PutPromotedSearch: %v", err)
	}

	got, err := c.GetPromotedSearch(req)
	if err != nil {
		t.Fatalf("GetPromotedSearch: %v", err)
	}
	if len(got) != 1 || got[0].ID != "stock-lookup" {
		t.Fatalf("expected limited cached stock result, got %+v", got)
	}
	if got[0].Offset == nil || *got[0].Offset != 0 {
		t.Fatalf("expected offset 0, got %+v", got[0].Offset)
	}
}
```

- [ ] **Step 4: Run promotion tests**

Run:

```bash
cd skillhub
go test ./pkg/cache -run 'TestPromotedSearchCache' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit promotion tests**

```bash
git add skillhub/pkg/cache/cache_test.go skillhub/pkg/cache/search_cache.go
git commit -m "test: cover promoted search cache promotion"
```

### Task 4: Wire MCP Search To Promoted Cache

**Files:**
- Modify: `skillhub/cmd/skillhub/main.go`
- Modify: `skillhub/cmd/skillhub/main_test.go`

- [ ] **Step 1: Extend fake discovery client**

Modify `fakeDiscoverySearchClient` in `skillhub/cmd/skillhub/main_test.go`:

```go
type fakeDiscoverySearchClient struct {
	called    bool
	callCount int
	req       discoveryclient.SearchRequest
	results   []discoveryclient.SkillSummary
}
```

Modify `Search`:

```go
func (f *fakeDiscoverySearchClient) Search(ctx context.Context, req discoveryclient.SearchRequest) ([]discoveryclient.SkillSummary, error) {
	f.called = true
	f.callCount++
	f.req = req
	if f.results != nil {
		return f.results, nil
	}
	return []discoveryclient.SkillSummary{{
		ID:          "remote",
		Name:        "Remote Skill",
		Description: "remote semantic result",
		Version:     "v1.0.0",
	}}, nil
}
```

- [ ] **Step 2: Replace old bypass test with first-search observation test**

Replace `TestMCPSearchBypassesLocalCache` with:

```go
func TestMCPSearchFirstCallUsesDiscoveryAndRecordsObservation(t *testing.T) {
	dir := t.TempDir()
	c, err := cachepkg.Open(filepath.Join(dir, "skillhub.db"), filepath.Join(dir, "skills"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	defer c.Close()

	client := &fakeDiscoverySearchClient{}
	core := &mcpCore{client: client, cache: c}

	results, err := core.Search(types.SearchRequest{Description: "local", Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !client.called {
		t.Fatal("expected discovery search to be called")
	}
	if client.req.Description != "local" || client.req.Limit != 5 {
		t.Fatalf("unexpected discovery request: %+v", client.req)
	}
	if len(results) != 1 || results[0].ID != "remote" {
		t.Fatalf("expected remote result, got %+v", results)
	}

	ok, err := c.ShouldPromoteSearch(types.SearchRequest{Description: "local", Limit: 5})
	if err != nil {
		t.Fatalf("ShouldPromoteSearch: %v", err)
	}
	if ok {
		t.Fatal("single observation should not promote")
	}
}
```

- [ ] **Step 3: Add promoted-hit test**

Append:

```go
func TestMCPSearchReturnsPromotedCacheHitWithoutDiscovery(t *testing.T) {
	dir := t.TempDir()
	c, err := cachepkg.Open(filepath.Join(dir, "skillhub.db"), filepath.Join(dir, "skills"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	defer c.Close()

	req := types.SearchRequest{Description: "小红书浦东租房补贴", Limit: 10}
	if err := c.PutPromotedSearch(req, []types.SkillSummary{{ID: "cached", Name: "Cached Skill"}}); err != nil {
		t.Fatalf("PutPromotedSearch: %v", err)
	}

	client := &fakeDiscoverySearchClient{}
	core := &mcpCore{client: client, cache: c}
	results, err := core.Search(req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if client.called {
		t.Fatal("promoted cache hit should not call discovery")
	}
	if len(results) != 1 || results[0].ID != "cached" {
		t.Fatalf("expected cached result, got %+v", results)
	}
}
```

- [ ] **Step 4: Add promote-then-refresh test**

Append:

```go
func TestMCPSearchStableObservationsRefreshesDiscoveryThenCaches(t *testing.T) {
	dir := t.TempDir()
	c, err := cachepkg.Open(filepath.Join(dir, "skillhub.db"), filepath.Join(dir, "skills"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	defer c.Close()

	for _, query := range []string{
		"小红书浦东租房补贴",
		"小红书明珠租房补贴",
		"浦东小红书租房补贴调研",
	} {
		err := c.RecordSearchObservation(
			types.SearchRequest{Description: query},
			[]types.SkillSummary{{ID: "xiaohongshu-browser"}, {ID: "policy-helper"}},
		)
		if err != nil {
			t.Fatalf("RecordSearchObservation: %v", err)
		}
	}

	req := types.SearchRequest{Description: "小红书浦东明珠租房补贴", Limit: 10}
	client := &fakeDiscoverySearchClient{results: []discoveryclient.SkillSummary{{
		ID: "fresh-xhs", Name: "Fresh XHS Skill", Description: "fresh", Version: "v1.0.1",
	}}}
	core := &mcpCore{client: client, cache: c}

	results, err := core.Search(req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if client.callCount != 1 {
		t.Fatalf("expected one discovery refresh, got %d", client.callCount)
	}
	if len(results) != 1 || results[0].ID != "fresh-xhs" {
		t.Fatalf("expected fresh discovery result, got %+v", results)
	}

	client.called = false
	results, err = core.Search(req)
	if err != nil {
		t.Fatalf("second search: %v", err)
	}
	if client.called {
		t.Fatal("second search should hit promoted cache")
	}
	if len(results) != 1 || results[0].ID != "fresh-xhs" {
		t.Fatalf("expected cached fresh result, got %+v", results)
	}
}
```

- [ ] **Step 5: Run MCP tests to verify failures before implementation**

Run:

```bash
cd skillhub
go test ./cmd/skillhub -run 'TestMCPSearch' -count=1
```

Expected: At least the promoted-hit and refresh tests fail until `mcpCore.Search` is wired.

- [ ] **Step 6: Implement `mcpCore.Search` cache flow**

Modify `mcpCore.Search` in `skillhub/cmd/skillhub/main.go`:

```go
func (c *mcpCore) Search(req types.SearchRequest) ([]types.SkillSummary, error) {
	if c.cache != nil {
		cached, err := c.cache.GetPromotedSearch(req)
		if err == nil && cached != nil {
			return cached, nil
		}
	}

	discReq := discoveryclient.SearchRequest{
		ID:          req.ID,
		Description: req.Description,
		Tag:         req.Tag,
		Limit:       req.Limit,
		Offset:      req.Offset,
	}
	remoteResults, err := c.client.Search(context.Background(), discReq)
	if err != nil {
		return nil, err
	}
	out := discoveryResultsToTypes(remoteResults)

	if c.cache != nil {
		if ok, err := c.cache.ShouldPromoteSearch(req); err == nil && ok {
			_ = c.cache.PutPromotedSearch(req, out)
		}
		_ = c.cache.RecordSearchObservation(req, out)
		for _, r := range out {
			_ = c.cache.Upsert(r, "remote")
		}
	}

	return out, nil
}

func discoveryResultsToTypes(remoteResults []discoveryclient.SkillSummary) []types.SkillSummary {
	out := make([]types.SkillSummary, len(remoteResults))
	for i, r := range remoteResults {
		out[i] = types.SkillSummary{
			ID:          r.ID,
			Name:        r.Name,
			Description: r.Description,
			Version:     r.Version,
			Tags:        r.Tags,
			Offset:      r.Offset,
		}
	}
	return out
}
```

Remove the old async goroutine. Search-cache writes are small SQLite writes and need deterministic tests.

- [ ] **Step 7: Run MCP search tests**

Run:

```bash
cd skillhub
go test ./cmd/skillhub -run 'TestMCPSearch' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit MCP wiring**

```bash
git add skillhub/cmd/skillhub/main.go skillhub/cmd/skillhub/main_test.go
git commit -m "feat: use promoted search cache in mcp search"
```

### Task 5: Full Verification And Cleanup

**Files:**
- Modify only if verification reveals issues in files touched above.

- [ ] **Step 1: Format Go files**

Run:

```bash
cd skillhub
gofmt -w pkg/cache/tokenizer.go pkg/cache/search_cache.go pkg/cache/cache.go pkg/cache/cache_test.go cmd/skillhub/main.go cmd/skillhub/main_test.go
```

- [ ] **Step 2: Run focused tests**

Run:

```bash
cd skillhub
go test ./pkg/cache ./cmd/skillhub -count=1
```

Expected: PASS.

- [ ] **Step 3: Run broad SkillHub tests**

Run:

```bash
cd skillhub
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 4: Build SkillHub**

Run:

```bash
cd skillhub
go build ./...
```

Expected: PASS.

- [ ] **Step 5: Inspect final diff**

Run:

```bash
git diff --stat
git diff -- skillhub/pkg/cache skillhub/cmd/skillhub skillhub/go.mod skillhub/go.sum
```

Expected: Diff only includes tokenizer, search-cache storage/scoring, MCP wiring, tests, and module dependency changes.

- [ ] **Step 6: Final commit if any verification fixes were needed**

If verification required fixes after the previous commits:

```bash
git add skillhub/pkg/cache skillhub/cmd/skillhub skillhub/go.mod skillhub/go.sum
git commit -m "fix: stabilize promoted search cache"
```

If no fixes were needed, skip this step.

---

## Self-Review

- Spec coverage: The plan implements SQLite FTS5/BM25 query observations, gse + embedded jieba dictionary tokenization, 3-observation promotion, weighted result stability, 24h promoted cache TTL, discovery refresh before promotion write, and load/skill cache separation.
- Scope boundary: The plan does not implement OpenClaw loaded injection; that remains a separate plugin-owned feature from the design.
- Regression guard: Existing `cache.Search` remains unchanged for dependency lookup and local skill metadata search. MCP `Search` changes only the remote-search path and uses discovery as fallback/freshness source.
- Placeholder scan: No placeholders or TBD items remain.
