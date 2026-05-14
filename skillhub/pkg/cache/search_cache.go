package cache

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"skillhub/pkg/types"
)

const promotedSearchTTL = 24 * time.Hour

type searchObservationResult struct {
	ID   string `json:"id"`
	Rank int    `json:"result_order"`
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
		`CREATE VIRTUAL TABLE IF NOT EXISTS promoted_search_results_fts USING fts5(
			cache_key UNINDEXED,
			result_order UNINDEXED,
			id UNINDEXED,
			name,
			description,
			version UNINDEXED,
			tags,
			tokens,
			expires_at UNINDEXED
		)`,
	}
	for _, stmt := range stmts {
		if _, err := c.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cache) searchPromotedResults(description, tag string, limit, offset int) ([]types.SkillSummary, error) {
	_, _ = c.db.Exec(`DELETE FROM promoted_search_results_fts WHERE expires_at <= ?`, time.Now().UTC().Format(time.RFC3339Nano))
	queryText := strings.TrimSpace(tag + " " + description)
	tokens := c.tokenizer.Tokens(queryText)
	if len(tokens) == 0 {
		return []types.SkillSummary{}, nil
	}
	rows, err := c.db.Query(
		`WITH best_cache AS (
		   SELECT cache_key
		   FROM promoted_search_results_fts
		   WHERE promoted_search_results_fts MATCH ? AND expires_at > ?
		   ORDER BY bm25(promoted_search_results_fts, 1.0, 1.0, 5.0, 4.0, 3.0, 1.0, 1.0, 4.0, 1.0)
		   LIMIT 1
		 )
		 SELECT id, name, description, version, tags
		 FROM promoted_search_results_fts
		 WHERE cache_key = (SELECT cache_key FROM best_cache)
		 ORDER BY CAST(result_order AS INTEGER)
		 LIMIT ? OFFSET ?`,
		ftsMatchExpr(tokens), time.Now().UTC().Format(time.RFC3339Nano), limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []types.SkillSummary
	for rows.Next() {
		summary, err := scanPromotedSummary(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range result {
		resultOffset := offset + i
		result[i].Offset = &resultOffset
	}
	if result == nil {
		return []types.SkillSummary{}, nil
	}
	return result, nil
}

func (c *Cache) PutPromotedSearch(req types.SearchRequest, results []types.SkillSummary) error {
	key := c.promotedSearchKey(req)
	if _, err := c.db.Exec(`DELETE FROM promoted_search_results_fts WHERE cache_key = ?`, key); err != nil {
		return err
	}
	expiresAt := time.Now().UTC().Add(promotedSearchTTL).Format(time.RFC3339Nano)
	intentTokens := strings.Join(c.searchTokens(req), " ")
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, result := range results {
		tagsJSON, _ := json.Marshal(result.Tags)
		if result.Tags == nil {
			tagsJSON = []byte("[]")
		}
		tokenText := strings.Join(c.tokenizer.Tokens(intentTokens+" "+result.ID+" "+result.Name+" "+result.Description+" "+strings.Join(result.Tags, " ")), " ")
		if _, err := tx.Exec(
			`INSERT INTO promoted_search_results_fts(cache_key, result_order, id, name, description, version, tags, tokens, expires_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			key, i, result.ID, result.Name, result.Description, result.Version, string(tagsJSON), tokenText, expiresAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
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
	rows, err := c.db.Query(
		`SELECT o.id, bm25(search_observations_fts) AS score, o.results_json
		 FROM search_observations_fts
		 JOIN search_observations o ON o.id = search_observations_fts.rowid
		 WHERE search_observations_fts MATCH ?
		 ORDER BY score
		 LIMIT ?`,
		ftsMatchExpr(tokens), limit,
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

func rankWeight(result_order int) int {
	switch result_order {
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

type promotedSummaryScanner interface {
	Scan(dest ...any) error
}

func scanPromotedSummary(scanner promotedSummaryScanner) (types.SkillSummary, error) {
	var s types.SkillSummary
	var tagsJSON string
	if err := scanner.Scan(&s.ID, &s.Name, &s.Description, &s.Version, &tagsJSON); err != nil {
		return types.SkillSummary{}, err
	}
	var tags []string
	_ = json.Unmarshal([]byte(tagsJSON), &tags)
	s.Tags = tags
	return s, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
