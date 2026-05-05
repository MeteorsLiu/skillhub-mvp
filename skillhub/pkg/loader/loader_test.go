package loader_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"skillhub/pkg/loader"
)

func TestLoadRootSkill(t *testing.T) {
	dir := filepath.Join("..", "parser", "testdata", "root-skill")
	skill, err := loader.LoadRoot(dir, "v1.4.2")
	if err != nil {
		t.Fatalf("LoadRoot failed: %v", err)
	}

	if skill.ID != "github.com/acme/clawhub/social/publish-post" {
		t.Errorf("expected id 'github.com/acme/clawhub/social/publish-post', got %q", skill.ID)
	}
	if skill.Name != "发布小红书图文" {
		t.Errorf("expected name '发布小红书图文', got %q", skill.Name)
	}
	if skill.Version != "v1.4.2" {
		t.Errorf("expected version 'v1.4.2', got %q", skill.Version)
	}
	if len(skill.SubSkills) != 2 {
		t.Fatalf("expected 2 sub-skills, got %d", len(skill.SubSkills))
	}

	subA := skill.SubSkills[0]
	if subA.ID != "github.com/acme/clawhub/social/publish-post/sub-a" {
		t.Errorf("expected sub-a id 'github.com/acme/clawhub/social/publish-post/sub-a', got %q", subA.ID)
	}
	if subA.Name != "Sub A" {
		t.Errorf("expected sub-a name 'Sub A', got %q", subA.Name)
	}
	if subA.Description != "Sub skill A" {
		t.Errorf("expected sub-a description 'Sub skill A', got %q", subA.Description)
	}
	if subA.Version != "v1.4.2" {
		t.Errorf("expected sub-a version 'v1.4.2', got %q", subA.Version)
	}
	if len(subA.Tags) != 1 || subA.Tags[0] != "writing" {
		t.Errorf("expected sub-a tags ['writing'], got %v", subA.Tags)
	}

	subB := skill.SubSkills[1]
	if subB.ID != "github.com/acme/clawhub/social/publish-post/sub-b" {
		t.Errorf("expected sub-b id 'github.com/acme/clawhub/social/publish-post/sub-b', got %q", subB.ID)
	}
	if subB.Name != "Sub B" {
		t.Errorf("expected sub-b name 'Sub B', got %q", subB.Name)
	}

	body := "# Publish Post\n\nContent body here.\n"
	if skill.Body != body {
		t.Errorf("expected body %q, got %q", body, skill.Body)
	}

	if len(skill.Deps.Tools) != 2 || skill.Deps.Tools[0] != "ffmpeg" || skill.Deps.Tools[1] != "yt-dlp" {
		t.Errorf("unexpected tools: %v", skill.Deps.Tools)
	}
	if len(skill.Deps.Skills) != 1 {
		t.Fatalf("expected 1 dep skill, got %d", len(skill.Deps.Skills))
	}
	if skill.Deps.Skills[0].ID != "github.com/acme/clawhub/common/image-tools" {
		t.Errorf("expected dep skill id 'github.com/acme/clawhub/common/image-tools', got %q", skill.Deps.Skills[0].ID)
	}
	if skill.Deps.Skills[0].Version != "v1.2.0" {
		t.Errorf("expected dep skill version 'v1.2.0', got %q", skill.Deps.Skills[0].Version)
	}
}

func TestLoadSubSkill(t *testing.T) {
	dir := filepath.Join("..", "parser", "testdata", "root-skill")
	skill, err := loader.LoadSub(dir, "sub-a", "github.com/acme/clawhub/social/publish-post", "v1.4.2")
	if err != nil {
		t.Fatalf("LoadSub failed: %v", err)
	}

	if skill.ID != "github.com/acme/clawhub/social/publish-post/sub-a" {
		t.Errorf("expected id 'github.com/acme/clawhub/social/publish-post/sub-a', got %q", skill.ID)
	}
	if skill.Name != "Sub A" {
		t.Errorf("expected name 'Sub A', got %q", skill.Name)
	}
	if skill.Version != "v1.4.2" {
		t.Errorf("expected version 'v1.4.2', got %q", skill.Version)
	}
	if len(skill.SubSkills) != 0 {
		t.Errorf("expected 0 sub-skills, got %d", len(skill.SubSkills))
	}
	if len(skill.Deps.Skills) != 0 {
		t.Errorf("expected 0 dep skills, got %d", len(skill.Deps.Skills))
	}
	if len(skill.Deps.Tools) != 0 {
		t.Errorf("expected 0 dep tools, got %d", len(skill.Deps.Tools))
	}

	body := "# Sub A\n"
	if skill.Body != body {
		t.Errorf("expected body %q, got %q", body, skill.Body)
	}
}

func TestLoadRoot_NoSubSkills(t *testing.T) {
	dir := filepath.Join("..", "parser", "testdata", "minimal")
	skill, err := loader.LoadRoot(dir, "v1.0.0")
	if err != nil {
		t.Fatalf("LoadRoot failed: %v", err)
	}

	if skill.ID != "github.com/bob/rednote-skill" {
		t.Errorf("expected id 'github.com/bob/rednote-skill', got %q", skill.ID)
	}
	if skill.Name != "Rednote Skill" {
		t.Errorf("expected name 'Rednote Skill', got %q", skill.Name)
	}
	if skill.Version != "v1.0.0" {
		t.Errorf("expected version 'v1.0.0', got %q", skill.Version)
	}
	if skill.Body != "# Minimal\n" {
		t.Errorf("expected body '# Minimal\\n', got %q", skill.Body)
	}
	if len(skill.SubSkills) != 0 {
		t.Errorf("expected 0 sub-skills, got %d", len(skill.SubSkills))
	}
}

func TestLoadRoot_JSON(t *testing.T) {
	dir := filepath.Join("..", "parser", "testdata", "root-skill")
	skill, err := loader.LoadRoot(dir, "v1.4.2")
	if err != nil {
		t.Fatalf("LoadRoot failed: %v", err)
	}

	data, err := json.MarshalIndent(skill, "", "  ")
	if err != nil {
		t.Fatalf("json marshal failed: %v", err)
	}

	got := string(data)

	if skill.ID != "github.com/acme/clawhub/social/publish-post" {
		t.Errorf("expected id in JSON, got %q", skill.ID)
	}
	if skill.Version != "v1.4.2" {
		t.Errorf("expected version in JSON, got %q", skill.Version)
	}

	var decoded struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Version   string `json:"version"`
		Body      string `json:"body"`
		SubSkills []any  `json:"sub_skills"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}
	if decoded.ID != "github.com/acme/clawhub/social/publish-post" {
		t.Errorf("expected id after round-trip, got %q", decoded.ID)
	}
	if decoded.Version != "v1.4.2" {
		t.Errorf("expected version after round-trip, got %q", decoded.Version)
	}
	if len(decoded.SubSkills) != 2 {
		t.Errorf("expected 2 sub-skills in JSON, got %d", len(decoded.SubSkills))
	}

	t.Logf("JSON output:\n%s", got)
}
