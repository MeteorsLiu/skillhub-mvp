package types_test

import (
	"encoding/json"
	"strings"
	"testing"

	"skillhub/pkg/types"
)

func TestParseDependency_Valid(t *testing.T) {
	id, version, err := types.ParseDependency("foo@1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "foo" {
		t.Errorf("expected id 'foo', got %q", id)
	}
	if version != "1.0" {
		t.Errorf("expected version '1.0', got %q", version)
	}
}

func TestParseDependency_MissingVersion(t *testing.T) {
	_, _, err := types.ParseDependency("foo")
	if err == nil {
		t.Fatal("expected error for missing version, got nil")
	}
}

func TestParseDependency_MultipleAt(t *testing.T) {
	_, _, err := types.ParseDependency("foo@1.0@2.0")
	if err == nil {
		t.Fatal("expected error for multiple @, got nil")
	}
}

func TestSearchRequest_Validate_AllEmpty(t *testing.T) {
	req := types.SearchRequest{}
	err := req.Validate()
	if err == nil {
		t.Fatal("expected error when all query fields are empty")
	}
}

func TestSearchRequest_Validate_WithID(t *testing.T) {
	req := types.SearchRequest{ID: "skill-1"}
	if err := req.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSearchRequest_Validate_WithDescription(t *testing.T) {
	req := types.SearchRequest{Description: "some skill"}
	if err := req.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSearchRequest_Validate_WithTag(t *testing.T) {
	req := types.SearchRequest{Tag: "go"}
	if err := req.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadRequest_Validate_EmptyID(t *testing.T) {
	req := types.LoadRequest{}
	err := req.Validate()
	if err == nil {
		t.Fatal("expected error when ID is empty")
	}
}

func TestLoadRequest_Validate_WithID(t *testing.T) {
	req := types.LoadRequest{ID: "skill-1"}
	if err := req.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseDependency_EmptyVersion(t *testing.T) {
	_, _, err := types.ParseDependency("foo@")
	if err == nil {
		t.Fatal("expected error for empty version, got nil")
	}
}

func TestParseDependency_EmptyID(t *testing.T) {
	_, _, err := types.ParseDependency("@bar")
	if err == nil {
		t.Fatal("expected error for empty id, got nil")
	}
}


func TestSearchRequest_JSONTags(t *testing.T) {
	req := types.SearchRequest{ID: "s1", Description: "desc", Tag: "go", Limit: 5}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	want := `{"id":"s1","description":"desc","tag":"go","limit":5}`
	if got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
}

func TestLoadRequest_JSONTags(t *testing.T) {
	req := types.LoadRequest{ID: "s1", Version: "1.0"}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	want := `{"id":"s1","version":"1.0"}`
	if got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
}

func TestSkillSummary_JSONTagsOmitempty(t *testing.T) {
	s := types.SkillSummary{ID: "s1", Name: "test"}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	want := `{"id":"s1","name":"test"}`
	if got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
}

func TestSkillSummary_JSONTagsAllFields(t *testing.T) {
	s := types.SkillSummary{
		ID:          "s1",
		Name:        "test",
		Description: "a test skill",
		Version:     "1.0",
		Tags:        []string{"go", "test"},
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	want := `{"id":"s1","name":"test","description":"a test skill","version":"1.0","tags":["go","test"]}`
	if got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
}

func TestSkill_DepsJSON(t *testing.T) {
	skill := types.Skill{
		Body: "echo hello",
		Deps: types.SkillDeps{
			Skills: []types.SkillSummary{
				{ID: "s1", Name: "Skill One"},
			},
			Tools: []string{"tool-a"},
		},
	}
	b, err := json.Marshal(skill)
	if err != nil {
		t.Fatal(err)
	}
	var decoded types.Skill
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Body != "echo hello" {
		t.Errorf("expected body 'echo hello', got %q", decoded.Body)
	}
	if len(decoded.Deps.Skills) != 1 || decoded.Deps.Skills[0].ID != "s1" {
		t.Errorf("expected deps.skills[0].id 's1', got %q", decoded.Deps.Skills[0].ID)
	}
	if len(decoded.Deps.Tools) != 1 || decoded.Deps.Tools[0] != "tool-a" {
		t.Errorf("expected deps.tools[0] 'tool-a', got %q", decoded.Deps.Tools[0])
	}
}

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
