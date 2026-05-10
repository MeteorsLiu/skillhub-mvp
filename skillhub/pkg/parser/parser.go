package parser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"skillhub/pkg/types"
)

type ParseResult struct {
	ID            string
	Name          string
	Description   string
	Tags          []string
	Deps          types.SkillDeps
	Body          string
	SubSkillPaths []string
}

type fmDeps struct {
	Tools  []string `yaml:"tools"`
	Skills []string `yaml:"skills"`
}

type frontmatter struct {
	ID           string   `yaml:"id"`
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	Tags         []string `yaml:"tags"`
	Dependencies fmDeps   `yaml:"dependencies"`
	SubSkills    []string `yaml:"skills"`
}

func ParseRoot(dir string) (*ParseResult, error) {
	result, err := ParseRootWithID(dir, "")
	if err != nil {
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

func ParseRootWithID(dir, defaultID string) (*ParseResult, error) {
	path := filepath.Join(dir, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	requireID := defaultID == ""
	result, err := parseData(data, requireID)
	if err != nil {
		return nil, err
	}

	if result.ID == "" && defaultID != "" {
		result.ID = defaultID
	}

	if len(result.SubSkillPaths) == 0 {
		subs, err := DiscoverSubSkills(dir)
		if err != nil {
			return nil, err
		}
		result.SubSkillPaths = subs
	}

	return result, nil
}

func ParseSubSkill(skillPath, parentID string) (*ParseResult, error) {
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", skillPath, err)
	}

	result, err := parseData(data, false)
	if err != nil {
		return nil, err
	}

	dirName := filepath.Base(filepath.Dir(skillPath))
	result.ID = parentID + "/" + dirName

	return result, nil
}

func parseData(data []byte, requireID bool) (*ParseResult, error) {
	content := string(data)
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("no frontmatter found")
	}

	var fm frontmatter
	if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
		return nil, fmt.Errorf("parsing frontmatter: %w", err)
	}

	if requireID && fm.ID == "" {
		return nil, fmt.Errorf("id is required in frontmatter")
	}

	deps, err := parseDeps(fm.Dependencies)
	if err != nil {
		return nil, err
	}

	body := strings.TrimSpace(parts[2]) + "\n"

	return &ParseResult{
		ID:            fm.ID,
		Name:          fm.Name,
		Description:   fm.Description,
		Tags:          fm.Tags,
		Deps:          deps,
		Body:          body,
		SubSkillPaths: fm.SubSkills,
	}, nil
}

func parseDeps(d fmDeps) (types.SkillDeps, error) {
	var skills []types.SkillSummary
	for _, s := range d.Skills {
		id, version, err := types.ParseDependency(s)
		if err != nil {
			return types.SkillDeps{}, fmt.Errorf("parsing dependency %q: %w", s, err)
		}
		skills = append(skills, types.SkillSummary{
			ID:      id,
			Version: version,
		})
	}
	return types.SkillDeps{
		Skills: skills,
		Tools:  d.Tools,
	}, nil
}

func DiscoverSubSkills(dir string) ([]string, error) {
	skillsDir := filepath.Join(dir, "skills")
	info, err := os.Stat(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("stat %s: %w", skillsDir, err)
	}
	if !info.IsDir() {
		return []string{}, nil
	}
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", skillsDir, err)
	}
	var result []string
	for _, e := range entries {
		if e.IsDir() {
			skillPath := filepath.Join(skillsDir, e.Name(), "SKILL.md")
			if _, err := os.Stat(skillPath); err == nil {
				result = append(result, e.Name())
			}
		}
	}
	return result, nil
}

func deriveIDFromGit(dir string) (string, error) {
	cmd := exec.Command("git", "-c", "credential.helper=", "-C", dir, "remote", "get-url", "origin")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git remote: %w", err)
	}
	url := strings.TrimSpace(string(out))
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "git@")
	url = strings.Replace(url, ":", "/", 1)
	url = strings.TrimSuffix(url, ".git")
	if idx := strings.LastIndex(url, "@"); idx >= 0 {
		url = url[idx+1:]
	}
	if url == "" {
		return "", fmt.Errorf("empty git remote url")
	}
	cmd = exec.Command("git", "-c", "credential.helper=", "-C", dir, "config", "--get", "skillhub.subdir")
	if out, err := cmd.CombinedOutput(); err == nil {
		subdir := strings.Trim(strings.TrimSpace(string(out)), "/")
		if subdir != "" {
			url = strings.TrimRight(url, "/") + "/" + subdir
		}
	}
	return url, nil
}
