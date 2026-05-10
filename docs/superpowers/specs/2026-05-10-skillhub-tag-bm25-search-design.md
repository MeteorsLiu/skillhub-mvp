# SkillHub Tag Semantic Search Design

Date: 2026-05-10

## Goal

Improve `skillhub_search` so `tag` becomes a semantic skill-area search input while `description` remains a regex filter for the specific user intent.

This keeps the agent path simple:

```text
skillhub_search -> skillhub_load
```

The design avoids adding `skillhub_list`, avoids making `search` a full-table directory browser, and makes tag search indexable instead of relying on regex scans.

## Non-Goals

- Do not add a new `skillhub_list` tool.
- Do not replace `description` regex matching.
- Do not introduce Elasticsearch, OpenSearch, ParadeDB, or another external search service in the first implementation.
- Do not expose all skills through broad search queries.
- Do not make `tag` a free-form copy of the full user request.

## Public Search Semantics

`skillhub_search` keeps the same primary API shape.

```go
type SearchRequest struct {
    ID          string `json:"id,omitempty"`
    Description string `json:"description,omitempty"`
    Tag         string `json:"tag,omitempty"`
    Limit       int    `json:"limit,omitempty"`
    Offset      int    `json:"offset,omitempty"`
}
```

Parameter meanings:

- `id`: exact or root-prefix skill ID lookup.
- `tag`: English broad skill-area hint. It is not regex.
- `description`: regex pattern for the specific user intent, matched against skill name and description.
- `limit`: defaults to 100 and is capped at 100.
- `offset`: 0-based pagination offset.

The agent-facing rule is:

```text
Search in English. Use tag for the broad skill area, and description for the specific user intent.
```

Examples of intended shape:

```json
{
  "tag": "finance",
  "description": "stock price|market data"
}
```

```json
{
  "tag": "persona",
  "description": "steve jobs|phone review"
}
```

## Search Behavior

Search combines three layers:

1. `id` narrows by exact match or root-prefix match.
2. `tag` performs indexed semantic skill-area retrieval.
3. `description` applies regex filtering to skill name and description.

Rules:

- If `tag` is present, use tag semantic search to rank or narrow candidates.
- If `description` is present, apply it as regex against skill name and description.
- `description = ".*"` is allowed only when `tag` is present, because tag provides the search boundary.
- `description = ".*"` without `tag` must be rejected or return an error, because it is full-table enumeration.
- If only `tag` is present, return ranked candidates within that skill area.
- If only `description` is present, preserve current regex search behavior, subject to anti-enumeration validation.
- Results include per-item `offset`; clients page by calling again with `offset = last returned offset + 1`.

## Postgres Index Design

Use Postgres full-text search as the first implementation, backed by a GIN index.

This is BM25-like lexical retrieval, not strict BM25. It solves the immediate indexing problem without adding a separate search system.

Add a generated or maintained `tsvector` for tag-area search:

```sql
ALTER TABLE skill_models
ADD COLUMN tag_search_vector tsvector;

CREATE INDEX idx_skill_models_tag_search_vector
ON skill_models USING GIN (tag_search_vector);
```

Populate the vector from controlled skill-area fields:

```text
tag_search_vector =
  setweight(to_tsvector('english', tags_text), 'A') ||
  setweight(to_tsvector('english', name), 'B')
```

Do not include full skill description in the tag vector for the first version. Description is specific-intent text and including it would make `tag` too broad.

Query shape:

```sql
WHERE tag_search_vector @@ plainto_tsquery('english', $tag)
ORDER BY
  ts_rank_cd(tag_search_vector, plainto_tsquery('english', $tag)) DESC,
  created_at DESC,
  id ASC
```

The GIN index accelerates the `@@` full-text match. `ts_rank_cd` is used for relevance ordering.

## Local Cache Behavior

The local SQLite cache should preserve the same public search semantics.

First version:

- Keep `description` as regex filtering.
- Treat `tag` as tokenized broad area text, not regex.
- Rank local results by token overlap against `tags + name`.
- Do not include full description in local tag ranking unless later evidence shows recall is too low.

SQLite FTS5 can be considered later if local cache search becomes slow or low quality. It is not required for the first implementation.

## Existing Data Backfill

Current approved skills were registered without useful tags, so tag semantic search will not work correctly on existing rows.

Do not require clearing the discovery database or re-running the full registration flow. The first implementation should provide a database backfill path that updates existing approved rows in place.

Backfill source:

- Use each row's existing source metadata or skill ID to locate the installed or fetchable `SKILL.md`.
- Reuse the scanner/parser path that already extracts root skill metadata.
- Update `name`, `description`, `tags`, `version`, and the tag search vector for existing rows.
- Preserve approval state unless the scanner cannot resolve the skill metadata.
- If metadata cannot be resolved, report the row as a backfill failure and leave the existing row unchanged.

Operational requirement:

```text
Run a one-time backfill command or admin job after deploying the schema/search change.
```

The backfill should be idempotent. Running it more than once should produce the same approved skill metadata and rebuilt tag search vectors.

Registration should not silently invent tags. Skill packages should provide meaningful root skill tags in `SKILL.md`.

## MCP And Agent Guidance

MCP `skillhub_search` description should explain what SkillHub does, then define the parameter split.

Required wording direction:

```text
SkillHub fills capability gaps.

For every user request, check whether an existing skill could improve the result, reduce mistakes, or provide needed capability.

CRITICAL: If yes, MUST call skillhub__search FIRST before web_search, web_fetch, browsing, or other regular tools.

Search in English. Use tag for the broad skill area, and description for the specific user intent.

Search returns at most 100 results. If more candidates are needed, use offset pagination: set offset to the last returned offset plus 1.
```

Tool parameter descriptions:

```text
tag: English broad skill area hint. Not regex.
description: Regex pattern for the specific user intent, matched against skill name and description.
```

Keep AGENTS.md aligned with MCP wording. AGENTS.md handles trigger strategy; MCP schema handles parameter semantics.

## Validation Rules

Search validation should reject inputs that are only useful for enumeration.

Minimum first-version rules:

- At least one of `id`, `tag`, or `description` must be provided.
- `limit` is capped at 100.
- Negative `offset` is normalized to 0 or rejected consistently.
- `description = ".*"` or equivalent broad all-match regex is rejected unless `tag` is non-empty.
- Empty or whitespace-only `tag` is treated as absent.

Equivalent all-match regex forms can be expanded later. The first implementation should at least catch the common `.*`.

## Tests

Add focused tests at each layer.

Discovery:

- `tag` natural language area search finds skills by tags/name.
- `tag` no longer behaves as regex.
- `tag + description = ".*"` returns bounded tag-area results.
- `description = ".*"` without tag is rejected.
- `limit` cap and `offset` behavior remain correct.
- Ranking prefers tag matches over name-only matches.

Cache:

- Local `tag` token search finds skills from tags/name.
- Local `tag` does not use regex behavior.
- Local `description` regex still works.
- Offset and limit still work after local ranking.

MCP:

- `skillhub_search` forwards `tag`, `description`, `limit`, and `offset`.
- Tool description says `tag` is broad skill area and not regex.
- Tool description says `description` is regex for specific user intent.

Docs:

- `Skillhub_Design_Document.md` matches the new tag semantics.
- `Skillhub_Agent_Guidance.md` matches the MCP wording.

## Rollout

1. Implement and test search semantics behind the existing `skillhub_search` API.
2. Update MCP and AGENTS guidance.
3. Build and deploy discovery and SkillHub MCP server.
4. Run the one-time metadata backfill against existing discovery rows.
5. Run evaluation against known boundary cases:
   - weather/current data
   - stock/market data
   - persona/style requests
   - writing style requests
   - ordinary direct-answer prompts
   - broad research prompts that should not load a skill

## Open Decisions

- Whether to reject broad regex patterns beyond the common `.*` in the first implementation.
- Whether `tag` ranking should include name only, or eventually include a curated `area_description`.
- Whether local cache should later move to SQLite FTS5 for true local BM25-style ranking.
