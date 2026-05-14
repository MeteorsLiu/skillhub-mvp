package cache

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"

	"skillhub/pkg/types"
)

type Cache struct {
	db        *sql.DB
	tokenizer *Tokenizer
}

func Open(dbPath, _ string) (*Cache, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	tok, err := NewTokenizer()
	if err != nil {
		return nil, fmt.Errorf("init tokenizer: %w", err)
	}
	c := &Cache{db: db, tokenizer: tok}
	if err := c.initSearchCacheSchema(); err != nil {
		return nil, fmt.Errorf("init search cache schema: %w", err)
	}
	return c, nil
}

func (c *Cache) Close() error {
	return c.db.Close()
}

func (c *Cache) Search(description, tag string, limit, offset int) ([]types.SkillSummary, error) {
	description = strings.TrimSpace(description)
	tag = strings.TrimSpace(tag)
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return c.searchPromotedResults(description, tag, limit, offset)
}
