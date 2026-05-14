package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	cachepkg "skillhub/pkg/cache"
	"skillhub/pkg/discoveryclient"
	"skillhub/pkg/types"
)

type fakeDiscoverySearchClient struct {
	called    bool
	callCount int
	req       discoveryclient.SearchRequest
	results   []discoveryclient.SkillSummary
}

func TestPrepareResourceDirectoryExcludesSkillMD(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("root instructions"), 0644); err != nil {
		t.Fatalf("write root skill: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(src, "references"), 0755); err != nil {
		t.Fatalf("mkdir references: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "references", "guide.md"), []byte("resource"), 0644); err != nil {
		t.Fatalf("write resource: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(src, "skills", "sub"), 0755); err != nil {
		t.Fatalf("mkdir sub skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "sub", "SKILL.md"), []byte("sub instructions"), 0644); err != nil {
		t.Fatalf("write sub skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "sub", "example.txt"), []byte("sub resource"), 0644); err != nil {
		t.Fatalf("write sub resource: %v", err)
	}

	rootID := "github.com/acme/resource-test"
	version := "v1.0.0"
	wantDir := filepath.Join(skillResourceRoot, "github.com", "acme", "resource-test@v1.0.0")
	t.Cleanup(func() {
		_ = os.RemoveAll(wantDir)
	})

	dir, err := prepareResourceDirectory(src, rootID, version)
	if err != nil {
		t.Fatalf("prepare resources: %v", err)
	}
	if dir != wantDir {
		t.Fatalf("resource dir = %q, want %q", dir, wantDir)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("root SKILL.md should be excluded, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "sub", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("sub SKILL.md should be excluded, stat err=%v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "references", "guide.md")); err != nil || string(data) != "resource" {
		t.Fatalf("resource not copied: data=%q err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "skills", "sub", "example.txt")); err != nil || string(data) != "sub resource" {
		t.Fatalf("sub resource not copied: data=%q err=%v", data, err)
	}
}

func TestCmdSearchUsesNaturalLanguageQuery(t *testing.T) {
	req := searchRequestFromQuery("zhangxuefeng")
	if req.ID != "" {
		t.Fatalf("CLI keyword search should not set ID filter, got %q", req.ID)
	}
	if req.Description != "zhangxuefeng" || req.Tag != "zhangxuefeng" || req.Limit != 20 {
		t.Fatalf("unexpected CLI search request: %+v", req)
	}
}

func TestSplitInstalledSubSkillUsesFilesystem(t *testing.T) {
	dir := t.TempDir()
	rootDir := filepath.Join(dir, "github.com", "acme", "repo", "v1.0.0")
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	root, sub := splitInstalledSubSkill(dir, "github.com/acme/repo/travel/planner")
	if root != "github.com/acme/repo" || sub != "travel/planner" {
		t.Fatalf("unexpected split root=%q sub=%q", root, sub)
	}
}

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

	cached, err := c.Search("local", "", 5, 0)
	if err != nil {
		t.Fatalf("cache search: %v", err)
	}
	if len(cached) != 0 {
		t.Fatalf("single observation should not promote, got %+v", cached)
	}
}

func TestMCPSearchReturnsBM25CacheHitWithoutDiscovery(t *testing.T) {
	dir := t.TempDir()
	c, err := cachepkg.Open(filepath.Join(dir, "skillhub.db"), filepath.Join(dir, "skills"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	defer c.Close()

	req := types.SearchRequest{Description: "小红书浦东租房补贴", Limit: 10}
	for i := 0; i < 3; i++ {
		if err := c.RecordSearch(req, []types.SkillSummary{{ID: "cached", Name: "Cached Skill"}}); err != nil {
			t.Fatalf("RecordSearch %d: %v", i, err)
		}
	}

	client := &fakeDiscoverySearchClient{}
	core := &mcpCore{client: client, cache: c}
	results, err := core.Search(req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if client.called {
		t.Fatal("BM25 cache hit should not call discovery")
	}
	if len(results) != 1 || results[0].ID != "cached" {
		t.Fatalf("expected cached result, got %+v", results)
	}
}

func TestMCPSearchStableObservationsCacheWithoutDiscovery(t *testing.T) {
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
		err := c.RecordSearch(
			types.SearchRequest{Description: query},
			[]types.SkillSummary{{ID: "xiaohongshu-browser"}, {ID: "policy-helper"}},
		)
		if err != nil {
			t.Fatalf("RecordSearch: %v", err)
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
	if client.callCount != 0 {
		t.Fatalf("expected stable cache hit without discovery, got %d calls", client.callCount)
	}
	if len(results) == 0 || results[0].ID != "xiaohongshu-browser" {
		t.Fatalf("expected cached xiaohongshu-browser result, got %+v", results)
	}
}
