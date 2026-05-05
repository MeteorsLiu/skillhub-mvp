package types

import (
	"errors"
	"strings"
)

type SearchRequest struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Tag         string `json:"tag"`
	Limit       int    `json:"limit"`
}

func (r SearchRequest) Validate() error {
	if r.ID == "" && r.Description == "" && r.Tag == "" {
		return errors.New("at least one of id, description, or tag must be provided")
	}
	return nil
}

type LoadRequest struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

func (r LoadRequest) Validate() error {
	if r.ID == "" {
		return errors.New("id must not be empty")
	}
	return nil
}

type SkillSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Version     string   `json:"version,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type SkillDeps struct {
	Skills []SkillSummary `json:"skills,omitempty"`
	Tools  []string       `json:"tools"`
}

type Skill struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Version   string        `json:"version"`
	Body      string        `json:"body"`
	SubSkills []SkillSummary `json:"sub_skills,omitempty"`
	Deps      SkillDeps     `json:"deps"`
}

type SkillHubTools interface {
	Search(req SearchRequest) ([]SkillSummary, error)
	Load(req LoadRequest) (*Skill, error)
}

func ParseDependency(s string) (id, version string, err error) {
	parts := strings.Split(s, "@")
	if len(parts) == 1 {
		return "", "", errors.New("missing version: expected format id@version")
	}
	if len(parts) > 2 {
		return "", "", errors.New("invalid dependency: multiple @ symbols")
	}
	if parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("invalid dependency: id and version must not be empty")
	}
	return parts[0], parts[1], nil
}

