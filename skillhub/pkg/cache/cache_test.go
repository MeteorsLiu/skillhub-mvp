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

	results, err := c.Search("Publish", "", 10, 0)
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

	results, err := c.Search("bar.*baz", "", 10, 0)
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

	results, err := c.Search("", "xiaohongshu", 10, 0)
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

	results, err := c.Search("", "finance market", 10, 0)
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

func TestSearch_OffsetPagination(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	skillsRoot := filepath.Join(dir, "skills")

	c, err := cache.Open(dbPath, skillsRoot)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer c.Close()

	for _, id := range []string{"a", "b", "c"} {
		c.Upsert(types.SkillSummary{
			ID: id, Name: "Skill " + id, Description: "common", Version: "v1.0.0",
		}, "local")
	}

	results, err := c.Search("common", "", 1, 1)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "b" {
		t.Errorf("expected id 'b', got %q", results[0].ID)
	}
	if results[0].Offset == nil || *results[0].Offset != 1 {
		t.Fatalf("expected offset 1, got %+v", results[0].Offset)
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

	results, err := c.Search("nonexistent", "", 10, 0)
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

	results, err := c.Search("Publish", "", 10, 0)
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

	results, _ := c.Search("New", "", 10, 0)
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
