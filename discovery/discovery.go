package discovery

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

const EmbeddingDimensions = 384

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

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type ReviewResult struct {
	Passed bool
	Reason string
	Detail string
}

type skillModel struct {
	ID          string  `gorm:"primaryKey"`
	Name        string  `gorm:"default:''"`
	Description string  `gorm:"default:''"`
	Version     string  `gorm:"default:''"`
	Tags        string  `gorm:"type:text[];default:'{}'"`
	Embedding   *string `gorm:"type:vector(384)"`
	Status      string  `gorm:"default:'pending'"`
	Source      string  `gorm:"default:''"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Discovery struct {
	db       *gorm.DB
	llm      LLMReviewer
	embedder Embedder
}

func New(db *gorm.DB, llm LLMReviewer) *Discovery {
	return &Discovery{db: db, llm: llm}
}

func NewWithEmbedder(db *gorm.DB, llm LLMReviewer, embedder Embedder) *Discovery {
	return &Discovery{db: db, llm: llm, embedder: embedder}
}

func (d *Discovery) Init(ctx context.Context) error {
	if err := d.db.WithContext(ctx).Exec(`CREATE EXTENSION IF NOT EXISTS vector`).Error; err != nil {
		return err
	}
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
	if !d.db.Migrator().HasColumn(&skillModel{}, "embedding") {
		if err := d.db.WithContext(ctx).Exec(fmt.Sprintf(`ALTER TABLE skill_models ADD COLUMN embedding vector(%d)`, EmbeddingDimensions)).Error; err != nil {
			return err
		}
	}
	if err := d.db.WithContext(ctx).Exec(`
		CREATE INDEX IF NOT EXISTS idx_skill_models_embedding
		ON skill_models USING hnsw (embedding vector_cosine_ops)
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

	if req.Description == "" && req.Tag == "" {
		return d.searchByID(ctx, req.ID, limit, offset)
	}

	return d.searchByEmbedding(ctx, req, limit, offset)
}

func (d *Discovery) searchByID(ctx context.Context, id string, limit, offset int) ([]SkillSummary, error) {
	q := d.db.WithContext(ctx).Model(&skillModel{}).
		Select("id, name, description, version, tags, status, source, created_at, updated_at").
		Where("status = ?", "approved").
		Where("id = ? OR id LIKE ?", id, id+"/%").
		Order("id ASC").
		Limit(limit).
		Offset(offset)

	var models []skillModel
	if err := q.Find(&models).Error; err != nil {
		return nil, err
	}
	return summariesFromModels(models, offset), nil
}

func (d *Discovery) searchByEmbedding(ctx context.Context, req SearchRequest, limit, offset int) ([]SkillSummary, error) {
	if d.embedder == nil {
		return nil, errors.New("semantic search requires embedding service")
	}
	vectors, err := d.embedder.Embed(ctx, []string{searchEmbeddingText(req)})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("embedding service returned %d vectors, expected 1", len(vectors))
	}
	vector, err := vectorLiteral(vectors[0])
	if err != nil {
		return nil, err
	}

	query := `
		SELECT id, name, description, version, tags, status, source, created_at, updated_at
		FROM skill_models
		WHERE status = ? AND embedding IS NOT NULL`
	args := []any{"approved"}
	if req.ID != "" {
		query += ` AND (id = ? OR id LIKE ?)`
		args = append(args, req.ID, req.ID+"/%")
	}
	query += `
		ORDER BY embedding <=> ?::vector ASC, created_at DESC, id ASC
		LIMIT ? OFFSET ?`
	args = append(args, vector, limit, offset)

	var models []skillModel
	if err := d.db.WithContext(ctx).Raw(query, args...).Scan(&models).Error; err != nil {
		return nil, err
	}
	return summariesFromModels(models, offset), nil
}

func summariesFromModels(models []skillModel, offset int) []SkillSummary {
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
	return results
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

func validateSearchRequest(req SearchRequest) error {
	if req.ID == "" && req.Description == "" && req.Tag == "" {
		return errors.New("at least one of id, description, or tag must be provided")
	}
	return nil
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
	if err := d.db.WithContext(ctx).Save(&m).Error; err != nil {
		return err
	}
	return d.updateEmbedding(ctx, skill.ID, skill)
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
	return d.updateEmbedding(ctx, skill.ID, skill)
}

func (d *Discovery) updateEmbedding(ctx context.Context, id string, skill SkillSummary) error {
	if d.embedder == nil {
		return nil
	}
	vectors, err := d.embedder.Embed(ctx, []string{skillEmbeddingText(skill)})
	if err != nil {
		return err
	}
	if len(vectors) != 1 {
		return fmt.Errorf("embedding service returned %d vectors, expected 1", len(vectors))
	}
	vector, err := vectorLiteral(vectors[0])
	if err != nil {
		return err
	}
	return d.db.WithContext(ctx).Exec(`UPDATE skill_models SET embedding = ?::vector WHERE id = ?`, vector, id).Error
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

func skillEmbeddingText(skill SkillSummary) string {
	var parts []string
	if skill.ID != "" {
		parts = append(parts, "id: "+skill.ID)
	}
	if skill.Name != "" {
		parts = append(parts, "name: "+skill.Name)
	}
	if skill.Description != "" {
		parts = append(parts, "description: "+skill.Description)
	}
	if len(skill.Tags) > 0 {
		parts = append(parts, "tags: "+strings.Join(skill.Tags, ", "))
	}
	return strings.Join(parts, "\n")
}

func searchEmbeddingText(req SearchRequest) string {
	var parts []string
	if req.Tag != "" {
		parts = append(parts, "tag: "+req.Tag)
	}
	if req.Description != "" {
		parts = append(parts, "intent: "+req.Description)
	}
	if req.ID != "" {
		parts = append(parts, "id: "+req.ID)
	}
	return strings.Join(parts, "\n")
}

func vectorLiteral(values []float32) (string, error) {
	if len(values) != EmbeddingDimensions {
		return "", fmt.Errorf("embedding has %d dimensions, expected %d", len(values), EmbeddingDimensions)
	}
	parts := make([]string, len(values))
	for i, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return "", fmt.Errorf("embedding contains invalid value at dimension %d", i)
		}
		parts[i] = strconv.FormatFloat(float64(value), 'g', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}
