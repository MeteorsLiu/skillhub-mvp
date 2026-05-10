package discovery

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SkillSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Tags        []string `json:"tags"`
	Offset      *int     `json:"offset,omitempty"`
}

type SearchRequest struct {
	ID          string `json:"id,omitempty"`
	Description string `json:"description,omitempty"`
	Tag         string `json:"tag,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	Offset      int    `json:"offset,omitempty"`
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
	ID              string `gorm:"primaryKey"`
	Name            string `gorm:"default:''"`
	Description     string `gorm:"default:''"`
	Version         string `gorm:"default:''"`
	Tags            string `gorm:"type:text[];default:'{}'"`
	TagSearchVector string `gorm:"type:tsvector"`
	Status          string `gorm:"default:'pending'"`
	Source          string `gorm:"default:''"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
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
	if !d.db.Migrator().HasColumn(&skillModel{}, "tag_search_vector") {
		if err := d.db.WithContext(ctx).Exec(`ALTER TABLE skill_models ADD COLUMN tag_search_vector tsvector`).Error; err != nil {
			return err
		}
	}
	if err := d.db.WithContext(ctx).Exec(`
		CREATE INDEX IF NOT EXISTS idx_skill_models_tag_search_vector
		ON skill_models USING GIN (tag_search_vector)
	`).Error; err != nil {
		return err
	}
	return nil
}

func (d *Discovery) Search(ctx context.Context, req SearchRequest) ([]SkillSummary, error) {
	req = trimSearchFields(req)
	if err := validateSearchRequest(req); err != nil {
		return nil, err
	}
	limit, offset := normalizeLimitOffset(req.Limit, req.Offset)

	q := d.db.WithContext(ctx).Model(&skillModel{}).Where("status = ?", "approved")

	if req.ID != "" {
		q = q.Where("id = ? OR id LIKE ?", req.ID, req.ID+"/%")
	}
	if req.Description != "" {
		pattern := searchPattern(req.Description)
		q = q.Where("name ~* ? OR description ~* ?", pattern, pattern)
	}
	if req.Tag != "" {
		q = q.Where("tag_search_vector @@ plainto_tsquery('english', ?)", req.Tag)
	}

	dbLimit := limit
	if len(searchTokens(req.Description)) > 1 && dbLimit < 50 {
		dbLimit = 50
	}
	dbLimit += offset

	var models []skillModel
	if req.Tag != "" {
		q = q.Order(clause.Expr{
			SQL:  "ts_rank_cd(tag_search_vector, plainto_tsquery('english', ?)) DESC",
			Vars: []any{req.Tag},
		})
	}
	if err := q.Order("created_at DESC, id ASC").Limit(dbLimit).Find(&models).Error; err != nil {
		return nil, err
	}
	if req.Tag == "" {
		rankModels(models, searchTokens(req.Description))
	}
	if offset >= len(models) {
		return []SkillSummary{}, nil
	}
	end := offset + limit
	if end > len(models) {
		end = len(models)
	}
	models = models[offset:end]

	results := make([]SkillSummary, len(models))
	for i, m := range models {
		resultOffset := offset + i
		results[i] = SkillSummary{
			ID:          m.ID,
			Name:        m.Name,
			Description: m.Description,
			Version:     m.Version,
			Tags:        parseTags(m.Tags),
			Offset:      &resultOffset,
		}
	}
	return results, nil
}

func normalizeLimitOffset(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func trimSearchFields(req SearchRequest) SearchRequest {
	req.ID = strings.TrimSpace(req.ID)
	req.Description = strings.TrimSpace(req.Description)
	req.Tag = strings.TrimSpace(req.Tag)
	return req
}

func isAllMatchDescription(description string) bool {
	return strings.TrimSpace(description) == ".*"
}

func validateSearchRequest(req SearchRequest) error {
	if req.ID == "" && req.Description == "" && req.Tag == "" {
		return errors.New("at least one of id, description, or tag must be provided")
	}
	if req.Tag == "" && isAllMatchDescription(req.Description) {
		return errors.New("description all-match regex requires tag")
	}
	return nil
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
		if !models[i].CreatedAt.Equal(models[j].CreatedAt) {
			return models[i].CreatedAt.After(models[j].CreatedAt)
		}
		return models[i].ID < models[j].ID
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

func tagSearchText(skill SkillSummary) (tagsText, nameText string) {
	return strings.Join(skill.Tags, " "), skill.Name
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
	if err := d.db.WithContext(ctx).Save(&m).Error; err != nil {
		return err
	}
	return d.updateTagSearchVector(ctx, skill.ID, skill)
}

func (d *Discovery) BackfillSkillMetadata(ctx context.Context, skill SkillSummary) error {
	skill.ID = strings.TrimSpace(skill.ID)
	if skill.ID == "" {
		return errors.New("id is required")
	}

	var existing skillModel
	if err := d.db.WithContext(ctx).Where("id = ?", skill.ID).First(&existing).Error; err != nil {
		return err
	}

	updates := map[string]any{
		"name":        skill.Name,
		"description": skill.Description,
		"version":     skill.Version,
		"tags":        joinTags(skill.Tags),
	}
	if err := d.db.WithContext(ctx).Model(&skillModel{}).Where("id = ?", skill.ID).Updates(updates).Error; err != nil {
		return err
	}
	return d.updateTagSearchVector(ctx, skill.ID, skill)
}

func (d *Discovery) updateTagSearchVector(ctx context.Context, id string, skill SkillSummary) error {
	tagsText, nameText := tagSearchText(skill)
	return d.db.WithContext(ctx).Exec(`
		UPDATE skill_models
		SET tag_search_vector =
			setweight(to_tsvector('english', coalesce(?, '')), 'A') ||
			setweight(to_tsvector('english', coalesce(?, '')), 'B')
		WHERE id = ?
	`, tagsText, nameText, id).Error
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
