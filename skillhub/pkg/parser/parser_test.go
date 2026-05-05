package parser_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"skillhub/pkg/parser"
)

func TestParseRootSkill(t *testing.T) {
	r, err := parser.ParseRoot("testdata/root-skill")
	if err != nil {
		t.Fatalf("ParseRoot failed: %v", err)
	}
	if r.ID != "github.com/acme/clawhub/social/publish-post" {
		t.Errorf("expected id 'github.com/acme/clawhub/social/publish-post', got %q", r.ID)
	}
	if r.Name != "发布小红书图文" {
		t.Errorf("expected name '发布小红书图文', got %q", r.Name)
	}
	if r.Description != "发布小红书图文内容" {
		t.Errorf("expected description '发布小红书图文内容', got %q", r.Description)
	}
	if len(r.Tags) != 2 || r.Tags[0] != "social" || r.Tags[1] != "xiaohongshu" {
		t.Errorf("unexpected tags: %v", r.Tags)
	}
	if len(r.Deps.Tools) != 2 || r.Deps.Tools[0] != "ffmpeg" || r.Deps.Tools[1] != "yt-dlp" {
		t.Errorf("unexpected tools: %v", r.Deps.Tools)
	}
	if len(r.Deps.Skills) != 1 {
		t.Fatalf("expected 1 dep skill, got %d", len(r.Deps.Skills))
	}
	if r.Deps.Skills[0].ID != "github.com/acme/clawhub/common/image-tools" {
		t.Errorf("expected dep skill id 'github.com/acme/clawhub/common/image-tools', got %q", r.Deps.Skills[0].ID)
	}
	if r.Deps.Skills[0].Version != "v1.2.0" {
		t.Errorf("expected version 'v1.2.0', got %q", r.Deps.Skills[0].Version)
	}
	if len(r.SubSkillPaths) != 2 {
		t.Fatalf("expected 2 sub-skill paths, got %d: %v", len(r.SubSkillPaths), r.SubSkillPaths)
	}
	body := "# Publish Post\n\nContent body here.\n"
	if r.Body != body {
		t.Errorf("expected body %q, got %q", body, r.Body)
	}
}

func TestParseRoot_MissingID(t *testing.T) {
	_, err := parser.ParseRoot("testdata/root-skill/skills/sub-a")
	if err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

func TestParseSubSkill(t *testing.T) {
	r, err := parser.ParseSubSkill("testdata/root-skill/skills/sub-a/SKILL.md", "github.com/acme/clawhub/social/publish-post")
	if err != nil {
		t.Fatalf("ParseSubSkill failed: %v", err)
	}
	if r.ID != "github.com/acme/clawhub/social/publish-post/sub-a" {
		t.Errorf("expected id 'github.com/acme/clawhub/social/publish-post/sub-a', got %q", r.ID)
	}
	if r.Name != "Sub A" {
		t.Errorf("expected name 'Sub A', got %q", r.Name)
	}
	if r.Description != "Sub skill A" {
		t.Errorf("expected description 'Sub skill A', got %q", r.Description)
	}
	if len(r.Tags) != 1 || r.Tags[0] != "writing" {
		t.Errorf("unexpected tags: %v", r.Tags)
	}
	if len(r.Deps.Tools) != 0 {
		t.Errorf("expected 0 dep tools, got %d", len(r.Deps.Tools))
	}
	if len(r.Deps.Skills) != 0 {
		t.Errorf("expected 0 dep skills, got %d", len(r.Deps.Skills))
	}
	if len(r.SubSkillPaths) != 0 {
		t.Errorf("expected 0 sub-skill paths, got %d", len(r.SubSkillPaths))
	}
	body := "# Sub A\n"
	if r.Body != body {
		t.Errorf("expected body %q, got %q", body, r.Body)
	}
}

func TestParseSubSkill_DerivedID(t *testing.T) {
	r, err := parser.ParseSubSkill("testdata/root-skill/skills/sub-b/SKILL.md", "github.com/acme/clawhub/social/publish-post")
	if err != nil {
		t.Fatalf("ParseSubSkill failed: %v", err)
	}
	expectedID := "github.com/acme/clawhub/social/publish-post/sub-b"
	if r.ID != expectedID {
		t.Errorf("expected derived id %q, got %q", expectedID, r.ID)
	}
}

func TestDiscoverSubSkills(t *testing.T) {
	skills, err := parser.DiscoverSubSkills("testdata/root-skill")
	if err != nil {
		t.Fatalf("DiscoverSubSkills failed: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 sub-skills, got %d: %v", len(skills), skills)
	}
	expected := []string{"sub-a", "sub-b"}
	for i, s := range expected {
		if skills[i] != s {
			t.Errorf("expected sub-skill %d to be %q, got %q", i, s, skills[i])
		}
	}
}

func TestParseFrontmatter(t *testing.T) {
	r, err := parser.ParseRoot("testdata/minimal")
	if err != nil {
		t.Fatalf("ParseRoot on minimal failed: %v", err)
	}
	if r.ID != "github.com/bob/rednote-skill" {
		t.Errorf("expected id 'github.com/bob/rednote-skill', got %q", r.ID)
	}
	if r.Name != "Rednote Skill" {
		t.Errorf("expected name 'Rednote Skill', got %q", r.Name)
	}
	if r.Description != "Post to rednote" {
		t.Errorf("expected description 'Post to rednote', got %q", r.Description)
	}
	if r.Body != "# Minimal\n" {
		t.Errorf("expected body '# Minimal\\n', got %q", r.Body)
	}
}

func TestDiscoverSubSkills_NoSkillsDir(t *testing.T) {
	skills, err := parser.DiscoverSubSkills("testdata/minimal")
	if err != nil {
		t.Fatalf("DiscoverSubSkills on minimal failed: %v", err)
	}
	if skills == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}
}

func TestParseRoot_IDFallbackFromGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	exec.Command("git", "init", dir).Run()
	exec.Command("git", "-C", dir, "-c", "credential.helper=", "remote", "add", "origin", "https://github.com/bob/test-skill").Run()

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
