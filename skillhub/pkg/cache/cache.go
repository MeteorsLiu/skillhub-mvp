package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"skillhub/pkg/types"
)

type Cache struct {
	db *sql.DB
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
	c := &Cache{db: db}
	if err := c.syncFromFS(skillsRoot); err != nil {
		return nil, fmt.Errorf("sync from filesystem: %w", err)
	}
	return c, nil
}

func (c *Cache) Close() error {
	return c.db.Close()
}

func (c *Cache) Search(description, tag string, limit int) ([]types.SkillSummary, error) {
	if limit <= 0 {
		limit = 20
	}

	var descRe *regexp.Regexp
	if description != "" {
		var err error
		descRe, err = regexp.Compile(description)
		if err != nil {
			return nil, fmt.Errorf("compile search regex: %w", err)
		}
	}

	var likePattern string
	if tag != "" {
		likePattern = tag
	} else if descRe == nil {
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
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
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
