package cache

import (
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"skillhub/pkg/types"
)

const promotedSearchTTL = 24 * time.Hour

type searchObservationResult struct {
	ID   string `json:"id"`
	Rank int    `json:"rank"`
}

type searchObservation struct {
	ID      int64
	Score   float64
	Results []searchObservationResult
}

type resultStats struct {
	id          string
	score       float64
	appearances int
	rankSum     int
}

func (c *Cache) initSearchCacheSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS search_observations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			query_text TEXT NOT NULL,
			query_tokens TEXT NOT NULL,
			results_json TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS search_observations_fts USING fts5(
			query_tokens,
			content='search_observations',
			content_rowid='id'
		)`,
		`CREATE TABLE IF NOT EXISTS promoted_search_cache (
			cache_key TEXT PRIMARY KEY,
			query_tokens TEXT NOT NULL,
			results_json TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			expires_at DATETIME NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := c.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cache) GetPromotedSearch(req types.SearchRequest) ([]types.SkillSummary, error) {
	key := c.promotedSearchKey(req)
	var raw string
	var expiresAt time.Time
	err := c.db.QueryRow(`SELECT results_json, expires_at FROM promoted_search_cache WHERE cache_key = ?`, key).Scan(&raw, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !time.Now().UTC().Before(expiresAt) {
		_, _ = c.db.Exec(`DELETE FROM promoted_search_cache WHERE cache_key = ?`, key)
		return nil, nil
	}
	var results []types.SkillSummary
	if err := json.Unmarshal([]byte(raw), &results); err != nil {
		return nil, err
	}
	return applyLimitOffset(results, req.Limit, req.Offset), nil
}

func (c *Cache) PutPromotedSearch(req types.SearchRequest, results []types.SkillSummary) error {
	key := c.promotedSearchKey(req)
	tokens := strings.Join(c.searchTokens(req), " ")
	raw, err := json.Marshal(results)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = c.db.Exec(
		`INSERT INTO promoted_search_cache (cache_key, query_tokens, results_json, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(cache_key) DO UPDATE SET
		   query_tokens = excluded.query_tokens,
		   results_json = excluded.results_json,
		   created_at = excluded.created_at,
		   expires_at = excluded.expires_at`,
		key, tokens, string(raw), now, now.Add(promotedSearchTTL),
	)
	return err
}

func (c *Cache) RecordSearchObservation(req types.SearchRequest, results []types.SkillSummary) error {
	if len(results) == 0 {
		return nil
	}
	tokens := c.searchTokens(req)
	if len(tokens) == 0 {
		return nil
	}
	obsResults := make([]searchObservationResult, 0, minInt(len(results), 5))
	for i, result := range results {
		if i >= 5 {
			break
		}
		obsResults = append(obsResults, searchObservationResult{ID: result.ID, Rank: i + 1})
	}
	raw, err := json.Marshal(obsResults)
	if err != nil {
		return err
	}
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tokenText := strings.Join(tokens, " ")
	res, err := tx.Exec(
		`INSERT INTO search_observations (query_text, query_tokens, results_json, created_at) VALUES (?, ?, ?, ?)`,
		c.searchText(req), tokenText, string(raw), time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	rowID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO search_observations_fts(rowid, query_tokens) VALUES (?, ?)`, rowID, tokenText); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Cache) ShouldPromoteSearch(req types.SearchRequest) (bool, error) {
	observations, err := c.similarObservations(req, 20)
	if err != nil {
		return false, err
	}
	return stableObservationResults(observations), nil
}

func (c *Cache) similarObservations(req types.SearchRequest, limit int) ([]searchObservation, error) {
	tokens := c.searchTokens(req)
	if len(tokens) == 0 {
		return nil, nil
	}
	expr := ftsMatchExpr(tokens)
	rows, err := c.db.Query(
		`SELECT o.id, bm25(search_observations_fts) AS score, o.results_json
		 FROM search_observations_fts
		 JOIN search_observations o ON o.id = search_observations_fts.rowid
		 WHERE search_observations_fts MATCH ?
		 ORDER BY score
		 LIMIT ?`,
		expr, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []searchObservation
	for rows.Next() {
		var obs searchObservation
		var raw string
		if err := rows.Scan(&obs.ID, &obs.Score, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &obs.Results); err != nil {
			return nil, err
		}
		out = append(out, obs)
	}
	return out, rows.Err()
}

func stableObservationResults(observations []searchObservation) bool {
	if len(observations) < 3 {
		return false
	}
	statsByID := map[string]*resultStats{}
	total := 0.0
	for _, obs := range observations {
		for _, result := range obs.Results {
			weight := rankWeight(result.Rank)
			if weight == 0 {
				continue
			}
			stats := statsByID[result.ID]
			if stats == nil {
				stats = &resultStats{id: result.ID}
				statsByID[result.ID] = stats
			}
			stats.score += float64(weight)
			stats.appearances++
			stats.rankSum += result.Rank
			total += float64(weight)
		}
	}
	if total == 0 {
		return false
	}
	stats := make([]*resultStats, 0, len(statsByID))
	for _, stat := range statsByID {
		stats = append(stats, stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].score != stats[j].score {
			return stats[i].score > stats[j].score
		}
		return stats[i].id < stats[j].id
	})
	best := stats[0]
	avgRank := float64(best.rankSum) / float64(best.appearances)
	return best.appearances >= 2 && avgRank <= 2 && best.score/total >= 0.40
}

func rankWeight(rank int) int {
	switch rank {
	case 1:
		return 5
	case 2:
		return 4
	case 3:
		return 3
	case 4:
		return 2
	case 5:
		return 1
	default:
		return 0
	}
}

func (c *Cache) searchTokens(req types.SearchRequest) []string {
	return c.tokenizer.Tokens(c.searchText(req))
}

func (c *Cache) searchText(req types.SearchRequest) string {
	return strings.TrimSpace(req.ID + " " + req.Tag + " " + req.Description)
}

func (c *Cache) promotedSearchKey(req types.SearchRequest) string {
	return strings.Join(c.searchTokens(req), " ")
}

func applyLimitOffset(results []types.SkillSummary, limit, offset int) []types.SkillSummary {
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(results) {
		return []types.SkillSummary{}
	}
	end := offset + limit
	if end > len(results) {
		end = len(results)
	}
	out := append([]types.SkillSummary(nil), results[offset:end]...)
	for i := range out {
		resultOffset := offset + i
		out[i].Offset = &resultOffset
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
