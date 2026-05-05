package loader

import (
	"fmt"
	"path/filepath"

	"skillhub/pkg/parser"
	"skillhub/pkg/types"
)

func LoadRoot(dir, version string) (*types.Skill, error) {
	result, err := parser.ParseRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("parsing root skill: %w", err)
	}

	skill := &types.Skill{
		ID:      result.ID,
		Name:    result.Name,
		Version: version,
		Body:    result.Body,
		Deps:    result.Deps,
	}

	var subSkills []types.SkillSummary
	for _, name := range result.SubSkillPaths {
		subPath := filepath.Join(dir, "skills", name, "SKILL.md")
		subResult, err := parser.ParseSubSkill(subPath, result.ID)
		if err != nil {
			return nil, fmt.Errorf("parsing sub-skill %q: %w", name, err)
		}
		subSkills = append(subSkills, types.SkillSummary{
			ID:          subResult.ID,
			Name:        subResult.Name,
			Description: subResult.Description,
			Version:     version,
			Tags:        subResult.Tags,
		})
	}
	if subSkills == nil {
		subSkills = []types.SkillSummary{}
	}
	skill.SubSkills = subSkills

	return skill, nil
}

func LoadSub(rootDir, subPath, parentID, version string) (*types.Skill, error) {
	fullPath := filepath.Join(rootDir, "skills", subPath, "SKILL.md")
	result, err := parser.ParseSubSkill(fullPath, parentID)
	if err != nil {
		return nil, fmt.Errorf("parsing sub-skill: %w", err)
	}

	return &types.Skill{
		ID:        result.ID,
		Name:      result.Name,
		Version:   version,
		Body:      result.Body,
		SubSkills: []types.SkillSummary{},
		Deps:      types.SkillDeps{Skills: []types.SkillSummary{}, Tools: []string{}},
	}, nil
}
