package discovery

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

type SkillSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Tags        []string `json:"tags"`
}

type SearchRequest struct {
	ID          string `json:"id,omitempty"`
	Description string `json:"description,omitempty"`
	Tag         string `json:"tag,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type RegisterRequest struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type LLMReviewer interface {
	Review(ctx context.Context, skill SkillSummary, body string) (*ReviewResult, error)
}

type ReviewResult struct {
	Passed bool
	Reason string
	Detail string
}

type skillModel struct {
	ID          string `gorm:"primaryKey"`
	Name        string `gorm:"default:''"`
	Description string `gorm:"default:''"`
	Version     string `gorm:"default:''"`
	Tags        string `gorm:"type:text[];default:'{}'"`
	Status      string `gorm:"default:'pending'"`
	Source      string `gorm:"default:''"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Discovery struct {
	db  *gorm.DB
	llm LLMReviewer
}

func New(db *gorm.DB, llm LLMReviewer) *Discovery {
	return &Discovery{db: db, llm: llm}
}

func (d *Discovery) Init(ctx context.Context) error {
	if err := d.db.WithContext(ctx).AutoMigrate(&skillModel{}); err != nil {
		return err
	}
	// Handle migration from old schema (approved bool → status string)
	if d.db.Migrator().HasColumn(&skillModel{}, "approved") {
		d.db.WithContext(ctx).Exec(`ALTER TABLE skill_models DROP COLUMN approved`)
	}
	if !d.db.Migrator().HasColumn(&skillModel{}, "status") {
		d.db.WithContext(ctx).Exec(`ALTER TABLE skill_models ADD COLUMN status TEXT NOT NULL DEFAULT 'pending'`)
	}
	return nil
}

func (d *Discovery) Search(ctx context.Context, req SearchRequest) ([]SkillSummary, error) {
	q := d.db.WithContext(ctx).Model(&skillModel{}).Where("status = ?", "approved")

	if req.ID != "" {
		q = q.Where("id = ? OR id LIKE ?", req.ID, req.ID+"/%")
	}
	if req.Description != "" {
		pattern := searchPattern(req.Description)
		q = q.Where("name ~* ? OR description ~* ?", pattern, pattern)
	}
	if req.Tag != "" {
		q = q.Where("EXISTS (SELECT 1 FROM unnest(tags) t WHERE t ~* ?)", req.Tag)
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	dbLimit := limit
	if len(searchTokens(req.Description)) > 1 && dbLimit < 50 {
		dbLimit = 50
	}

	var models []skillModel
	if err := q.Order("created_at DESC").Limit(dbLimit).Find(&models).Error; err != nil {
		return nil, err
	}
	rankModels(models, searchTokens(req.Description))
	if len(models) > limit {
		models = models[:limit]
	}

	results := make([]SkillSummary, len(models))
	for i, m := range models {
		results[i] = SkillSummary{
			ID:          m.ID,
			Name:        m.Name,
			Description: m.Description,
			Version:     m.Version,
			Tags:        parseTags(m.Tags),
		}
	}
	return results, nil
}

func searchPattern(description string) string {
	parts := searchTokens(description)
	if len(parts) > 1 {
		for i, part := range parts {
			parts[i] = regexp.QuoteMeta(part)
		}
		return strings.Join(parts, "|")
	}
	return description
}

func searchTokens(description string) []string {
	fields := strings.Fields(description)
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		token := strings.Trim(field, " \t\r\n,.;:!?\"'`()[]{}<>")
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func rankModels(models []skillModel, tokens []string) {
	if len(tokens) == 0 {
		return
	}
	sort.SliceStable(models, func(i, j int) bool {
		left := modelScore(models[i], tokens)
		right := modelScore(models[j], tokens)
		if left != right {
			return left > right
		}
		return models[i].CreatedAt.After(models[j].CreatedAt)
	})
}

func modelScore(m skillModel, tokens []string) int {
	nameID := strings.ToLower(m.Name + " " + m.ID)
	text := strings.ToLower(nameID + " " + m.Description)
	score := 0
	for _, token := range tokens {
		token = strings.ToLower(token)
		if strings.Contains(nameID, token) {
			score += 3
		} else if strings.Contains(text, token) {
			score++
		}
	}
	return score
}

func parseTags(s string) []string {
	if s == "" || s == "{}" {
		return nil
	}
	s = strings.Trim(s, "{}")
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(p, "\" ")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func joinTags(tags []string) string {
	if len(tags) == 0 {
		return "{}"
	}
	escaped := make([]string, len(tags))
	for i, t := range tags {
		escaped[i] = `"` + t + `"`
	}
	return "{" + strings.Join(escaped, ",") + "}"
}

func (d *Discovery) RegisterSkill(ctx context.Context, skill SkillSummary) error {
	m := skillModel{
		ID:          skill.ID,
		Name:        skill.Name,
		Description: skill.Description,
		Version:     skill.Version,
		Tags:        joinTags(skill.Tags),
		Status:      "pending",
	}
	return d.db.WithContext(ctx).Save(&m).Error
}

func (d *Discovery) Approve(ctx context.Context, id string) error {
	return d.db.WithContext(ctx).Model(&skillModel{}).Where("id = ?", id).Update("status", "approved").Error
}

func (d *Discovery) Reject(ctx context.Context, id string) error {
	return d.db.WithContext(ctx).Model(&skillModel{}).Where("id = ?", id).Update("status", "rejected").Error
}

func (d *Discovery) ListPending(ctx context.Context) ([]RegisterRequest, error) {
	var models []skillModel
	if err := d.db.WithContext(ctx).Model(&skillModel{}).Where("status = ?", "pending").Find(&models).Error; err != nil {
		return nil, err
	}
	reqs := make([]RegisterRequest, len(models))
	for i, m := range models {
		reqs[i] = RegisterRequest{ID: m.ID, Version: m.Version}
	}
	return reqs, nil
}
