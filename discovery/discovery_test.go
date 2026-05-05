package discovery_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"discovery"
)

func connectTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("SKILLHUB_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SKILLHUB_TEST_DATABASE_URL not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func freshTable(t *testing.T, d *discovery.Discovery, db *gorm.DB) {
	t.Helper()
	if err := d.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.WithContext(context.Background()).Exec("TRUNCATE skills")
	})
}

func TestInit(t *testing.T) {
	db := connectTestDB(t)
	d := discovery.New(db, nil)
	ctx := context.Background()

	db.WithContext(ctx).Exec("DROP TABLE IF EXISTS skills")

	if err := d.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	var exists bool
	db.WithContext(ctx).Raw("SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'skills')").Scan(&exists)
	if !exists {
		t.Fatal("skills table was not created")
	}

	if err := d.Init(ctx); err != nil {
		t.Fatalf("Init (second call) failed: %v", err)
	}
}

func TestRegisterAndSearch(t *testing.T) {
	db := connectTestDB(t)
	d := discovery.New(db, nil)
	ctx := context.Background()
	freshTable(t, d, db)

	skill := discovery.SkillSummary{
		ID: "test/skill-1", Name: "Test Skill", Description: "A test skill",
		Version: "1.0", Tags: []string{"test", "go"},
	}
	if err := d.RegisterSkill(ctx, skill); err != nil {
		t.Fatalf("RegisterSkill failed: %v", err)
	}

	results, err := d.Search(ctx, discovery.SearchRequest{ID: "test/skill-1"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results (not approved), got %d", len(results))
	}

	if err := d.Approve(ctx, "test/skill-1"); err != nil {
		t.Fatal(err)
	}

	results, err = d.Search(ctx, discovery.SearchRequest{ID: "test/skill-1"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "test/skill-1" {
		t.Errorf("expected ID 'test/skill-1', got %q", results[0].ID)
	}
	if results[0].Name != "Test Skill" {
		t.Errorf("expected Name 'Test Skill', got %q", results[0].Name)
	}
	if results[0].Version != "1.0" {
		t.Errorf("expected Version '1.0', got %q", results[0].Version)
	}
	if len(results[0].Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(results[0].Tags))
	}
}

func TestSearchByDescription(t *testing.T) {
	db := connectTestDB(t)
	d := discovery.New(db, nil)
	ctx := context.Background()
	freshTable(t, d, db)

	skills := []discovery.SkillSummary{
		{ID: "s1", Name: "S1", Description: "hello world", Version: "1.0"},
		{ID: "s2", Name: "S2", Description: "goodbye world", Version: "1.0"},
	}
	for _, s := range skills {
		if err := d.RegisterSkill(ctx, s); err != nil {
			t.Fatal(err)
		}
		if err := d.Approve(ctx, s.ID); err != nil {
			t.Fatal(err)
		}
	}

	results, err := d.Search(ctx, discovery.SearchRequest{Description: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "s1" {
		t.Errorf("expected 's1', got %q", results[0].ID)
	}
}

func TestSearchByTag(t *testing.T) {
	db := connectTestDB(t)
	d := discovery.New(db, nil)
	ctx := context.Background()
	freshTable(t, d, db)

	skills := []discovery.SkillSummary{
		{ID: "s1", Name: "S1", Tags: []string{"go", "test"}},
		{ID: "s2", Name: "S2", Tags: []string{"python", "test"}},
	}
	for _, s := range skills {
		if err := d.RegisterSkill(ctx, s); err != nil {
			t.Fatal(err)
		}
		if err := d.Approve(ctx, s.ID); err != nil {
			t.Fatal(err)
		}
	}

	results, err := d.Search(ctx, discovery.SearchRequest{Tag: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "s1" {
		t.Errorf("expected 's1', got %q", results[0].ID)
	}
}

func TestSearchPrefixID(t *testing.T) {
	db := connectTestDB(t)
	d := discovery.New(db, nil)
	ctx := context.Background()
	freshTable(t, d, db)

	skills := []discovery.SkillSummary{
		{ID: "github.com/user/repo", Name: "Root"},
		{ID: "github.com/user/repo/sub-a", Name: "Sub A"},
		{ID: "github.com/user/repo/sub-b", Name: "Sub B"},
		{ID: "github.com/other/repo", Name: "Other"},
	}
	for _, s := range skills {
		if err := d.RegisterSkill(ctx, s); err != nil {
			t.Fatal(err)
		}
		if err := d.Approve(ctx, s.ID); err != nil {
			t.Fatal(err)
		}
	}

	results, err := d.Search(ctx, discovery.SearchRequest{ID: "github.com/user/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].ID != "github.com/user/repo" {
		t.Errorf("expected root id, got %q", results[0].ID)
	}
}

func TestSearchOnlyApproved(t *testing.T) {
	db := connectTestDB(t)
	d := discovery.New(db, nil)
	ctx := context.Background()
	freshTable(t, d, db)

	skills := []discovery.SkillSummary{
		{ID: "s1", Name: "S1", Description: "common"},
		{ID: "s2", Name: "S2", Description: "common"},
	}
	for _, s := range skills {
		if err := d.RegisterSkill(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Approve(ctx, "s2"); err != nil {
		t.Fatal(err)
	}

	results, err := d.Search(ctx, discovery.SearchRequest{Description: "common"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "s2" {
		t.Errorf("expected 's2', got %q", results[0].ID)
	}
}

func TestApprove(t *testing.T) {
	db := connectTestDB(t)
	d := discovery.New(db, nil)
	ctx := context.Background()
	freshTable(t, d, db)

	if err := d.RegisterSkill(ctx, discovery.SkillSummary{ID: "s1", Name: "S1"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Approve(ctx, "s1"); err != nil {
		t.Fatal(err)
	}

	results, err := d.Search(ctx, discovery.SearchRequest{ID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after approve, got %d", len(results))
	}

	if err := d.Reject(ctx, "s1"); err != nil {
		t.Fatal(err)
	}

	results, err = d.Search(ctx, discovery.SearchRequest{ID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results after reject, got %d", len(results))
	}
}

func TestCombinedSearch(t *testing.T) {
	db := connectTestDB(t)
	d := discovery.New(db, nil)
	ctx := context.Background()
	freshTable(t, d, db)

	skills := []discovery.SkillSummary{
		{ID: "repo/skill-a", Name: "A", Description: "hello world", Tags: []string{"go"}},
		{ID: "repo/skill-b", Name: "B", Description: "hello world", Tags: []string{"python"}},
		{ID: "repo/skill-c", Name: "C", Description: "goodbye world", Tags: []string{"go"}},
	}
	for _, s := range skills {
		if err := d.RegisterSkill(ctx, s); err != nil {
			t.Fatal(err)
		}
		if err := d.Approve(ctx, s.ID); err != nil {
			t.Fatal(err)
		}
	}

	results, err := d.Search(ctx, discovery.SearchRequest{Description: "hello", Tag: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "repo/skill-a" {
		t.Errorf("expected 'repo/skill-a', got %q", results[0].ID)
	}
}

func TestSearchDefaultLimit(t *testing.T) {
	db := connectTestDB(t)
	d := discovery.New(db, nil)
	ctx := context.Background()
	freshTable(t, d, db)

	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("skill-%d", i)
		if err := d.RegisterSkill(ctx, discovery.SkillSummary{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
		if err := d.Approve(ctx, id); err != nil {
			t.Fatal(err)
		}
	}

	results, err := d.Search(ctx, discovery.SearchRequest{Description: "skill"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 20 {
		t.Fatalf("expected 20 results (default limit), got %d", len(results))
	}
}

func TestRegisterUpdatesExisting(t *testing.T) {
	db := connectTestDB(t)
	d := discovery.New(db, nil)
	ctx := context.Background()
	freshTable(t, d, db)

	if err := d.RegisterSkill(ctx, discovery.SkillSummary{ID: "s1", Name: "Original", Version: "1.0"}); err != nil {
		t.Fatal(err)
	}
	if err := d.RegisterSkill(ctx, discovery.SkillSummary{ID: "s1", Name: "Updated", Version: "2.0", Tags: []string{"new"}}); err != nil {
		t.Fatal(err)
	}
	if err := d.Approve(ctx, "s1"); err != nil {
		t.Fatal(err)
	}

	results, err := d.Search(ctx, discovery.SearchRequest{ID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "Updated" {
		t.Errorf("expected Name 'Updated', got %q", results[0].Name)
	}
	if results[0].Version != "2.0" {
		t.Errorf("expected Version '2.0', got %q", results[0].Version)
	}
	if len(results[0].Tags) != 1 || results[0].Tags[0] != "new" {
		t.Errorf("expected tags ['new'], got %v", results[0].Tags)
	}
}
