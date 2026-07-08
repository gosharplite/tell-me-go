# ADR-047: GitHub Code Search as skills.sh Discovery Mechanism

## Status
Accepted

## Context

The skills.sh ecosystem indexes skills from GitHub repositories containing `SKILL.md` files. The `search_skills` tool needs a discovery mechanism that can find skills matching a user's query without maintaining our own index or requiring API keys from skills.sh.

## Decision

Use the **GitHub code search API** (`/search/code`) with the query pattern `SKILL.md <query> in:file path:skills` as the primary discovery mechanism.

### Rationale

- **Zero infrastructure:** No index to maintain, no database to sync, no periodic crawl jobs.
- **Authentication model:** Works unauthenticated (with lower rate limits) or with a `GITHUB_TOKEN` environment variable for higher limits.
- **Always fresh:** Search results reflect the current state of all public GitHub repositories — no staleness window.
- **Incremental enhancement path:** Single-word queries use GitHub's built-in fuzzy matching; a future semantic search backed by embeddings could layer on top without changing the tool interface.

## Consequences

### Positive

- No new infrastructure or services to deploy and monitor.
- `search_skills` works immediately after `tell-me-go` is installed — no API key configuration required for basic usage.
- Discovery stays close to the source of truth (GitHub repositories).

### Negative / Effort

- **Rate limits:** Unauthenticated: ~10 req/min. Authenticated (with `GITHUB_TOKEN`): ~30 req/min. Acceptable for an interactive tool where searches are user-initiated, but a burst of rapid searches could hit the limit.
- **No quality ranking:** GitHub code search relevance does not correlate with skill quality or community adoption. Mitigation: results are presented as a flat list; the skills.sh leaderboard (web) provides the ranking layer.
- **`main` branch assumption:** `fetchSkillMeta` hardcodes `raw.githubusercontent.com/<owner>/<repo>/main/<path>`. Repositories using `master` or other default branch names silently degrade to "(no description)" in the search results. A future enhancement should use the repository's default branch from the search API response (`item.repository.default_branch`).

## Alternatives Considered

- **skills.sh REST API:** Rejected — requires Vercel OIDC authentication, which ties the tool to a specific deployment platform and authentication model.
- **Local npm-style registry:** Rejected — introduces infrastructure (registry service, sync jobs, storage) that must be maintained. Not worth the operational cost for a discovery mechanism.
- **GitHub topics search:** Rejected — too coarse-grained; finds repositories with a topic tag but cannot search within `SKILL.md` content for specific matching.
