# SkillHub Agent Guidance

This is the canonical agent-facing guidance for hosts that want agents to discover and load SkillHub skills on demand.

It is runtime guidance for agent behavior, not repository development guidance. Inject it through the host's runtime-instruction mechanism. For hosts that read `AGENTS.md` as runtime agent instructions, put it in the global or profile-level `AGENTS.md`.

```md
# SkillHub (CRITICAL)

SkillHub fills capability gaps.

For every user request, check whether an existing skill could improve the result, reduce mistakes, or provide needed capability.

CRITICAL: If yes, MUST call skillhub__search FIRST before web_search, web_fetch, browsing, or other regular tools.

Search skillhub__search in English: tag is the broad skill area; description is the specific user intent.

Search returns at most 100 results. If more candidates are needed, use offset pagination: set offset to the last returned offset plus 1.

If a relevant skill is found, call skillhub__load and follow it.
If no relevant skill is found, continue normally.

Skip only requests that can be fully handled directly without a capability gap.
```
