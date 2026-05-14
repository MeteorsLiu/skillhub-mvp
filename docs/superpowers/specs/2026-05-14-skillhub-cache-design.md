# SkillHub Cache Design

## Goal

Add a simple cache layer that makes SkillHub faster and helps an agent reuse skills already loaded in the current session, without turning cached skills into installed skills or globally injecting them into prompts.

## Non-Goals

- Do not add semantic route caching.
- Do not inject globally cached skills into prompts.
- Do not inject full `SKILL.md` content from cache.
- Do not let local cache permanently replace cloud discovery for new skill discovery.

## Architecture

Use a hybrid implementation:

- MCP owns promoted search cache and filesystem skill package resolution.
- The OpenClaw plugin owns session-level loaded-skill injection.

This keeps cache ownership aligned with responsibilities. MCP owns SkillHub tool execution, promoted search cache, and filesystem skill package resolution. SQLite cache storage is only for search observations and promoted BM25 results. The plugin owns prompt lifecycle hooks, so it should own what gets injected into the session context.

## Search Cache

Search cache is a promoted local cache inside the MCP server. It uses SQLite FTS5/BM25 for fast local recall, but it does not cache every remote search result. A search result becomes cacheable only after repeated similar searches show stable remote results.

Storage:

```text
$SKILLHUB_HOME/skillhub.db
```

Use the existing local SQLite cache database and add dedicated search observation and promoted result storage there. `SKILLHUB_HOME` keeps its current default of `$HOME/.skillhub`.

Tokenization:

```text
gse + embedded jieba official dictionary
```

Use Chinese-aware tokenization before writing query text into FTS. ASCII tokens are lowercased and preserved so tool names, APIs, and English skill terms still match.

Observation value:

```text
query tokens
top result ids with ranks
createdAt
```

Promoted cache value:

```text
results returned by discovery
intent tokens used for promotion
createdAt
expiresAt
```

Promotion rule:

```text
similar_observations >= 3
best_result appearance_count >= 2
best_result average_rank <= 2
best_result weighted_score / total_weighted_score >= 0.40
```

Similarity is found by querying the observation FTS table with the current query tokens. Result stability is checked by normalizing the remote top results across similar observations. Rank weights are:

```text
rank1 = 5
rank2 = 4
rank3 = 3
rank4 = 2
rank5 = 1
```

For each similar observation, add the rank weight to that result id. A result is stable only if it appears repeatedly, ranks near the top, and owns enough of the total weighted score. This prevents two superficially similar queries with different remote results from sharing a cache entry.

Behavior:

```text
search(query):
  search promoted result cache with BM25
  if matching promoted results exist and are not expired:
    return cached results

  find similar observations with BM25
  if promotion rule is satisfied:
    call discovery once
    write latest discovery results to promoted cache with 24h TTL
    write observation
    return fresh results

  call discovery
  write observation only
  return fresh results
```

The promoted result TTL is 24 hours. This is long enough to avoid repeated discovery calls for stable high-frequency searches, but short enough to let new or better skills surface from cloud discovery.

## Skill Package Cache

Skill packages are cached on disk by `load`, but they are not managed by the SQLite cache module. The SQLite cache is only for search observations and promoted BM25 search results.

Storage:

```text
$SKILLHUB_HOME/skills/{id-path}/{version}/
```

Use the existing local skill directory layout as the skill cache. Resource preparation continues to use `/tmp/.llar/{id}@{version}/` for non-`SKILL.md` resources.

Cache key:

```text
skill id + version
```

Behavior:

```text
load(id):
  resolve the requested skill version
  if $SKILLHUB_HOME/skills/{id-path}/{version}/ exists:
    parse and load from disk
  else:
    fetch into that path, then parse and load
```

Sub-skill resolution also uses the installed skill directory layout. It should not depend on SQLite metadata.

## Loaded Injection

Loaded injection is session-scoped and owned by the OpenClaw plugin.

When a SkillHub `load` call succeeds, the plugin records a compact entry in session state:

```json
{
  "id": "github.com/example/skill",
  "name": "example-skill",
  "summary": "short capability summary",
  "version": "1.0.0",
  "loadedAt": 1778745600000,
  "loadedTurn": 12,
  "loadedCompactionSeq": 1
}
```

Only `id`, `name`, and `summary` are injected into prompts. Full skill instructions still come from `load`.

Prompt shape:

```text
SkillHub skills loaded in this session:
- github.com/example/skill (example-skill): short capability summary

If relevant, continue using these loaded skills. Search SkillHub if none fits.
```

Budget:

```text
budgetChars = max(modelContextWindow * 5%, 4096)
```

`modelContextWindow` comes from plugin config when configured. If it is not configured or not exposed by the host, use `4096` characters. This keeps the first implementation independent of OpenClaw prompt-build event changes.

Injection rules:

- No hard item-count limit.
- Sort by most recent load first.
- Deduplicate by skill id; keep the newest load for each id.
- Add entries until the character budget is reached.
- Do not inject full `SKILL.md` content.
- Do not inject global skill-cache entries that were not loaded in the current session.

Compaction aging:

```text
session state keeps currentCompactionSeq
after_compaction increments currentCompactionSeq
loaded skills with currentCompactionSeq - loadedCompactionSeq > 3 leave injection candidates
```

This makes loaded injection a short-term context protection mechanism. If compaction preserves a loaded skill, extra injection is no longer needed. If compaction does not preserve it, the skill is probably no longer important enough to keep injecting. Reloading the same skill refreshes `loadedCompactionSeq` and returns it to injection candidates.

Cleanup:

- `session_end` clears loaded injection state.
- `before_reset` clears loaded injection state.

## Data Flow

Search:

```text
agent calls skillhub search
MCP checks promoted search cache
BM25 promoted hit -> return cached results
promoted miss -> check observation BM25 stability
stable repeated query -> call discovery -> store 24h promoted result -> write observation -> return fresh results
not stable -> call discovery -> write observation only -> return fresh results
```

Load:

```text
agent calls skillhub load(id)
MCP resolves id/version
MCP checks $SKILLHUB_HOME/skills/{id-path}/{version}/
disk hit -> parse and return
disk miss -> fetch to disk -> parse and return
plugin observes successful load and records session loaded entry
```

Prompt build:

```text
plugin reads session loaded entries
plugin filters entries older than 3 compactions
plugin formats id/name/summary entries within budget
plugin injects the loaded-skill list into session context
```

## Failure Handling

- Search cache failures must not fail `search`; fall back to discovery.
- Disk package cache read failures must not fail `load`; fetch and parse normally when possible.
- Loaded injection failures must not fail prompt construction; skip the injection and log.
- If context-window size is unknown, use the `4096` character budget.

## Testing

MCP tests:

- Search observations are written after discovery search.
- Promoted search cache is not written for the first matching search.
- Similar repeated searches promote results after 3 stable observations.
- Similar searches with split remote results do not promote cache entries.
- Promoted search cache returns cached results within the 24h TTL.
- Promoted search cache calls discovery after TTL expiry.
- Query tokenization handles Chinese terms and ASCII tool/API names.
- `load` resolves installed root and sub-skill paths from the filesystem.
- `load` does not depend on SQLite metadata to identify installed skills.
- Cache read/write failures fall back without breaking search/load.

Plugin tests:

- Successful `load` records a session loaded entry.
- Prompt injection includes only id/name/summary.
- Injection budget uses `max(contextWindow * 5%, 4096)`.
- Injection has no hard item-count limit and stops at the character budget.
- Duplicate skill ids keep the latest load.
- Entries older than 3 compactions are excluded from injection.
- `session_end` and `before_reset` clear injection state.
