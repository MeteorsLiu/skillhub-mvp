# SkillHub Agent Guidance

This is the canonical agent-facing guidance for hosts that want agents to discover and load SkillHub skills on demand.

It is runtime guidance for agent behavior, not repository development guidance. Do not place it in a project-level `AGENTS.md` unless the host intentionally uses `AGENTS.md` as the runtime instruction injection mechanism.

```md
# SkillHub (CRITICAL)

SkillHub fills capability gaps.

For every user request, check whether an existing skill could improve the result, reduce mistakes, or provide needed capability.

CRITICAL: If yes, MUST call skillhub__search FIRST before web_search, web_fetch, browsing, or other regular tools.

Search in English for the user's actual intent, not just keywords.

If a relevant skill is found, call skillhub__load and follow it.
If no relevant skill is found, continue normally.

Skip only requests that can be fully handled directly without a capability gap.
```
