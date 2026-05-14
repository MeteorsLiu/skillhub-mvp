package cache_test

import (
	"os"
	"path/filepath"
	"testing"

	"skillhub/pkg/cache"
	"skillhub/pkg/types"
)

func openTestCache(t *testing.T) *cache.Cache {
	t.Helper()
	dir := t.TempDir()
	c, err := cache.Open(filepath.Join(dir, "test.db"), filepath.Join(dir, "skills"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	return c
}

func TestOpen_CreatesDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	c, err := cache.Open(dbPath, filepath.Join(dir, "skills"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer c.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("db file not created")
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

func TestPromotedSearchCache_DoesNotPromoteFirstObservation(t *testing.T) {
	c := openTestCache(t)
	defer c.Close()

	req := types.SearchRequest{Description: "小红书浦东明珠租房补贴", Tag: "xiaohongshu", Limit: 10}
	results := []types.SkillSummary{{ID: "xiaohongshu-browser", Name: "XHS Browser", Description: "research xiaohongshu"}}

	if got, err := c.Search(req.Description, req.Tag, req.Limit, req.Offset); err != nil {
		t.Fatalf("Search: %v", err)
	} else if len(got) != 0 {
		t.Fatalf("expected no promoted hit before observation, got %+v", got)
	}

	if err := c.RecordSearch(req, results); err != nil {
		t.Fatalf("RecordSearch: %v", err)
	}

	if got, err := c.Search(req.Description, req.Tag, req.Limit, req.Offset); err != nil {
		t.Fatalf("Search: %v", err)
	} else if len(got) != 0 {
		t.Fatalf("first observation should not promote, got %+v", got)
	}
}

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
		{{ID: "xiaohongshu-browser"}, {ID: "policy-helper"}, {ID: "web-research"}},
		{{ID: "xiaohongshu-browser"}, {ID: "policy-helper"}, {ID: "web-research"}},
	}

	for i := range reqs {
		if err := c.RecordSearch(reqs[i], resultSets[i]); err != nil {
			t.Fatalf("record search %d: %v", i, err)
		}
	}

	got, err := c.Search("帮我调研小红书浦东租房补贴", "", 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) == 0 || got[0].ID != "xiaohongshu-browser" {
		t.Fatalf("expected stable observations to promote xiaohongshu-browser, got %+v", got)
	}
}

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
		if err := c.RecordSearch(reqs[i], resultSets[i]); err != nil {
			t.Fatalf("record search %d: %v", i, err)
		}
	}

	got, err := c.Search("小红书浦东明珠租房补贴", "", 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("split remote results should not promote, got %+v", got)
	}
}

func TestPromotedSearchCache_ReturnsWithinTTL(t *testing.T) {
	c := openTestCache(t)
	defer c.Close()

	req := types.SearchRequest{Description: "股票查询 A股 港股 美股 实时行情", Limit: 1}
	results := []types.SkillSummary{
		{ID: "stock-lookup", Name: "Stock Lookup", Tags: []string{"finance"}},
		{ID: "market-news", Name: "Market News"},
	}
	for i := 0; i < 3; i++ {
		if err := c.RecordSearch(req, results); err != nil {
			t.Fatalf("RecordSearch %d: %v", i, err)
		}
	}

	got, err := c.Search(req.Description, req.Tag, req.Limit, req.Offset)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].ID != "stock-lookup" {
		t.Fatalf("expected limited cached stock result, got %+v", got)
	}
	if got[0].Offset == nil || *got[0].Offset != 0 {
		t.Fatalf("expected offset 0, got %+v", got[0].Offset)
	}
	if len(got[0].Tags) != 1 || got[0].Tags[0] != "finance" {
		t.Fatalf("expected tags to round-trip, got %+v", got[0].Tags)
	}
}

func TestPromotedSearchCache_RegexLikeQueryDoesNotEnumerateRows(t *testing.T) {
	c := openTestCache(t)
	defer c.Close()

	for i := 0; i < 3; i++ {
		if err := c.RecordSearch(
			types.SearchRequest{Description: "stock lookup"},
			[]types.SkillSummary{{ID: "stock-lookup", Name: "Stock Lookup"}},
		); err != nil {
			t.Fatalf("RecordSearch %d: %v", i, err)
		}
	}

	got, err := c.Search(".*", "", 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected regex-like query not to enumerate rows, got %+v", got)
	}
}
