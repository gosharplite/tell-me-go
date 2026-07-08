# ADR-049: Duplicate Skill Name Resolution Policy

## Status
Accepted

## Context

Skills can have the same `name` field from different sources. For example, both `docs/skills/golang-patterns/SKILL.md` (curated) and a cloned `.skills/community-repo/skills/golang-patterns/SKILL.md` (third-party) could define a skill named `golang-patterns`. The `CompositeRepository`, which merges multiple `SkillRepository` instances, must have a deterministic policy for resolving which skill "wins" when names collide.

The name is the primary identifier used by the `SkillSelector` for keyword matching and by `remove_skill` for targeting. Non-deterministic resolution would cause unpredictable behavior: a user might install a skill that appears to work, only to find it silently overridden on the next reload.

## Decision

**First-wins with `docs/skills/` priority.** The `CompositeRepository` merges repositories in explicit registration order. `docs/skills/` (the curated local set) is registered first:

```go
Repos: []domain_skills.SkillRepository{fileRepo, skillsShRepo}
```

Within each individual repository's `reload()` method, duplicate detection uses `hasSkillName()` which checks the in-memory cache for an existing entry with the same name. The first skill encountered with a given name is retained; subsequent ones are logged at WARN level and skipped.

### Rationale

- **Curated > Community:** `docs/skills/` is curated by the Dobby template authors and shipped with the repository. A cloned community skill should not silently override a curated one. Registration order encodes this priority explicitly.
- **Predictability:** Registration order is explicit and visible at the composition root (`internal/infrastructure/di/container.go:buildSharedSkillRepo`). There are no hidden priority rules or scoring heuristics.
- **Observability:** Duplicates are logged at WARN level (`slog.Warn("duplicate skill name detected, skipping", ...)`), so operators and developers can detect conflicts without scraping logs at DEBUG level.
- **Minimal cognitive overhead:** First-wins is the simplest possible policy. More complex policies (e.g., "highest version wins," "most recently updated wins") would require version or timestamp metadata that skills do not currently carry.

## Consequences

### Positive

- Deterministic behavior is guaranteed by construction — registration order is fixed in code, not dependent on filesystem ordering or external state.
- Curated skills are protected from accidental override by community skills.
- WARN-level logging makes conflicts discoverable during development and troubleshooting.

### Negative / Effort

- To override a `docs/skills/` skill with a `.skills/` version, the operator must explicitly remove the local skill first. This is an intentional friction point — the override should be a deliberate act, not a side effect of installation order.
- If two `.skills/` repositories both define the same skill name, the winner depends on filesystem walk order (`filepath.Walk`), which is **non-deterministic across operating systems and filesystem implementations**. This is acceptable under the assumption that installing two repos with conflicting skill names is a user error — the user chose both repos. The WARN log makes the conflict visible, and `remove_skill` provides the remediation path.
- The policy does not support intentional shadowing (e.g., "install this community version of `golang-testing` to override the bundled one"). This is currently out of scope; if demand arises, a future ADR could introduce an explicit `priority` field or a `.skills/`-first ordering option.
