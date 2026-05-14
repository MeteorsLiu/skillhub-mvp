package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"skillhub/pkg/types"
)

type Cache struct {
	db        *sql.DB
	tokenizer *Tokenizer
}

func Open(dbPath, skillsRoot string) (*Cache, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS skills (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL DEFAULT '',
		description TEXT NOT NULL DEFAULT '',
		version     TEXT NOT NULL DEFAULT '',
		tags        TEXT NOT NULL DEFAULT '[]',
		status      TEXT NOT NULL DEFAULT '',
		source      TEXT NOT NULL DEFAULT '',
		created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return nil, fmt.Errorf("create table: %w", err)
	}
	tok, err := NewTokenizer()
	if err != nil {
		return nil, fmt.Errorf("init tokenizer: %w", err)
	}
	c := &Cache{db: db, tokenizer: tok}
	if err := c.initSearchCacheSchema(); err != nil {
		return nil, fmt.Errorf("init search cache schema: %w", err)
	}
	if err := c.syncFromFS(skillsRoot); err != nil {
		return nil, fmt.Errorf("sync from filesystem: %w", err)
	}
	return c, nil
}

func (c *Cache) Close() error {
	return c.db.Close()
}

func (c *Cache) Search(description, tag string, limit, offset int) ([]types.SkillSummary, error) {
	description = strings.TrimSpace(description)
	tag = strings.TrimSpace(tag)
	if err := validateSearch(description, tag); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var descRe *regexp.Regexp
	if description != "" {
		pattern := description
		parts := searchTokens(description)
		if len(parts) > 1 {
			for i, part := range parts {
				parts[i] = regexp.QuoteMeta(part)
			}
			pattern = strings.Join(parts, "|")
		}
		var err error
		descRe, err = regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("compile search regex: %w", err)
		}
	}

	var likePattern string
	if tag == "" && descRe == nil {
		likePattern = description
	}

	var rows *sql.Rows
	var err error
	if likePattern != "" {
		rows, err = c.db.Query(
			`SELECT id, name, description, version, tags FROM skills
			 WHERE (name || ' ' || description) LIKE '%' || ? || '%' OR tags LIKE '%' || ? || '%'
			 ORDER BY id`,
			likePattern, likePattern,
		)
	} else {
		rows, err = c.db.Query(
			`SELECT id, name, description, version, tags FROM skills ORDER BY id`,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var result []types.SkillSummary
	for rows.Next() {
		var s types.SkillSummary
		var tagsJSON string
		if err := rows.Scan(&s.ID, &s.Name, &s.Description, &s.Version, &tagsJSON); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		var tags []string
		json.Unmarshal([]byte(tagsJSON), &tags)
		s.Tags = tags
		result = append(result, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if descRe != nil {
		var filtered []types.SkillSummary
		for _, s := range result {
			if descRe.MatchString(s.Name) || descRe.MatchString(s.Description) {
				filtered = append(filtered, s)
			}
		}
		result = filtered
	}

	if result == nil {
		result = []types.SkillSummary{}
	}
	if tag != "" {
		tokens := tagTokens(tag)
		var filtered []types.SkillSummary
		for _, s := range result {
			if tagScore(s, tokens) > 0 {
				filtered = append(filtered, s)
			}
		}
		result = filtered
		sort.SliceStable(result, func(i, j int) bool {
			left := tagScore(result[i], tokens)
			right := tagScore(result[j], tokens)
			if left != right {
				return left > right
			}
			return result[i].ID < result[j].ID
		})
	} else {
		rankSummaries(result, searchTokens(description))
	}
	if offset >= len(result) {
		return []types.SkillSummary{}, nil
	}
	end := offset + limit
	if end > len(result) {
		end = len(result)
	}
	result = result[offset:end]
	for i := range result {
		resultOffset := offset + i
		result[i].Offset = &resultOffset
	}
	return result, nil
}

func validateSearch(description, tag string) error {
	if tag == "" && description == ".*" {
		return fmt.Errorf("description all-match regex requires tag")
	}
	return nil
}

func tagTokens(tag string) []string {
	return searchTokens(strings.ToLower(tag))
}

func tagScore(summary types.SkillSummary, tokens []string) int {
	if len(tokens) == 0 {
		return 0
	}
	tagText := strings.ToLower(strings.Join(summary.Tags, " "))
	nameText := strings.ToLower(summary.Name)
	score := 0
	for _, token := range tokens {
		if strings.Contains(tagText, token) {
			score += 3
		}
		if strings.Contains(nameText, token) {
			score++
		}
	}
	return score
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

func rankSummaries(summaries []types.SkillSummary, tokens []string) {
	if len(tokens) == 0 {
		return
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		left := summaryScore(summaries[i], tokens)
		right := summaryScore(summaries[j], tokens)
		if left != right {
			return left > right
		}
		return summaries[i].ID < summaries[j].ID
	})
}

func summaryScore(summary types.SkillSummary, tokens []string) int {
	nameID := strings.ToLower(summary.Name + " " + summary.ID)
	text := strings.ToLower(nameID + " " + summary.Description)
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

func (c *Cache) Upsert(summary types.SkillSummary, source string) error {
	tagsJSON, _ := json.Marshal(summary.Tags)
	if summary.Tags == nil {
		tagsJSON = []byte("[]")
	}
	_, err := c.db.Exec(
		`INSERT INTO skills (id, name, description, version, tags, source, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name = excluded.name,
		   description = excluded.description,
		   version = excluded.version,
		   tags = excluded.tags,
		   source = excluded.source,
		   updated_at = excluded.updated_at`,
		summary.ID, summary.Name, summary.Description, summary.Version,
		string(tagsJSON), source, time.Now().UTC(),
	)
	return err
}

func (c *Cache) AllRootIDs() ([]string, error) {
	rows, err := c.db.Query(`SELECT id FROM skills ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (c *Cache) syncFromFS(skillsRoot string) error {
	if skillsRoot == "" {
		return nil
	}
	return filepath.WalkDir(skillsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Name() == "SKILL.md" {
			content, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			summary := parseSummary(content, path, skillsRoot)
			if summary.ID != "" {
				c.Upsert(summary, "local")
			}
		}
		return nil
	})
}

func parseSummary(content []byte, path, skillsRoot string) types.SkillSummary {
	s := string(content)
	parts := strings.SplitN(s, "---", 3)
	if len(parts) < 3 {
		return types.SkillSummary{}
	}
	var fm struct {
		ID          string
		Name        string
		Description string
	}
	lines := strings.Split(parts[1], "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "id:") {
			fm.ID = strings.TrimSpace(line[3:])
		} else if strings.HasPrefix(line, "name:") {
			fm.Name = strings.TrimSpace(line[5:])
		} else if strings.HasPrefix(line, "description:") {
			fm.Description = strings.TrimSpace(line[12:])
		}
	}
	if fm.ID == "" {
		return types.SkillSummary{}
	}

	var version string
	rel, _ := filepath.Rel(skillsRoot, path)
	parts2 := strings.Split(rel, string(filepath.Separator))
	for _, p := range parts2 {
		if strings.HasPrefix(p, "v") {
			if matched, _ := regexp.MatchString(`^v\d+\.\d+\.\d+`, p); matched {
				version = p
			}
		}
	}

	return types.SkillSummary{
		ID:          fm.ID,
		Name:        fm.Name,
		Description: fm.Description,
		Version:     version,
	}
}
