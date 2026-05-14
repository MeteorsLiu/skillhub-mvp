# SkillHub Cache Design

## Goal

Add a simple cache layer that makes SkillHub faster and helps an agent reuse skills already loaded in the current session, without turning cached skills into installed skills or globally injecting them into prompts.

## Non-Goals

- Do not add semantic route caching.
- Do not add BM25 or a local search index in this version.
- Do not inject globally cached skills into prompts.
- Do not inject full `SKILL.md` content from cache.
- Do not replace cloud discovery as the source of fresh search results beyond the short search-cache TTL.

## Architecture

Use a hybrid implementation:

- MCP owns data caches for `search` and `load`.
- The OpenClaw plugin owns session-level loaded-skill injection.

This keeps cache ownership aligned with responsibilities. MCP already owns SkillHub tool execution and skill package resolution, so it should own search and load caches. The plugin owns prompt lifecycle hooks, so it should own what gets injected into the session context.

## Search Cache

Search cache is a strict short-TTL cache inside the MCP server.

Storage:

```text
$SKILLHUB_HOME/skillhub.db
```

Use the existing local SQLite cache database and add dedicated search-cache storage there. `SKILLHUB_HOME` keeps its current default of `$HOME/.skillhub`.

Cache key:

```text
normalized(id + tag + description + limit + offset)
```

Cache value:

```text
search results returned by discovery
createdAt
expiresAt
```

Behavior:

```text
cache hit and not expired -> return cached results
cache miss or expired -> call discovery -> write cache -> return fresh results
```

The TTL should be short. Five minutes is the initial default. This cache only avoids repeated identical or near-identical searches over a short period; it must not become a long-lived local discovery replacement.

## Skill Cache

Skill cache is a long-lived cache inside the MCP server.

Storage:

```text
$SKILLHUB_HOME/skills/{id-path}/{version}/
```

Use the existing local skill directory layout as the skill cache. Resource preparation continues to use `/tmp/.llar/{id}@{version}/` for non-`SKILL.md` resources.

Cache key:

```text
skill id + version
```

Cache value:

```text
loaded skill payload
parsed metadata
sub_skills metadata when present
resource cache metadata
createdAt
lastAccessedAt
```

Behavior:

```text
load(id):
  resolve the requested skill version
  if cache contains id + version:
    return cached payload
  else:
    fetch/parse/prepare resources
    write cache
    return fresh payload
```

If a later load resolves the same `id` to a different `version`, the MCP server should stop using the old entry for that id and write a new entry. The old entry may be deleted immediately or pruned by cache cleanup; it must not be returned for the new version.

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
MCP checks search cache
cache hit -> return cached results
cache miss -> call discovery -> store short-TTL result -> return
```

Load:

```text
agent calls skillhub load(id)
MCP resolves id/version
MCP checks skill cache
cache hit -> return cached load payload
cache miss -> fetch/parse/prepare -> store -> return
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
- Skill cache read failures must not fail `load`; fetch and parse normally.
- Skill cache write failures should be logged but must not fail `load`.
- Loaded injection failures must not fail prompt construction; skip the injection and log.
- If context-window size is unknown, use the `4096` character budget.

## Testing

MCP tests:

- Search cache returns cached results within TTL.
- Search cache calls discovery after TTL expiry.
- Search cache key includes id, tag, description, limit, and offset.
- Skill cache returns cached payload for the same id/version.
- Skill cache does not return an old version when the resolved version changes.
- Cache read/write failures fall back without breaking search/load.

Plugin tests:

- Successful `load` records a session loaded entry.
- Prompt injection includes only id/name/summary.
- Injection budget uses `max(contextWindow * 5%, 4096)`.
- Injection has no hard item-count limit and stops at the character budget.
- Duplicate skill ids keep the latest load.
- Entries older than 3 compactions are excluded from injection.
- `session_end` and `before_reset` clear injection state.
