# ADR-049: Third-Party Skill Injection Security Model

## Status
Accepted

## Context

The `install_skill` tool clones arbitrary third-party GitHub repositories into `.skills/`. The content of those repositories — the `SKILL.md` files — is subsequently injected into the LLM system prompt via the `SkillSelector`. This is a prompt-injection surface: a malicious skill could instruct the model to exfiltrate data, execute dangerous commands, bypass safety filters, or behave adversarially.

The system must balance the utility of community-contributed skills against the inherent risks of injecting unverified third-party text into the model's context.

## Decision

Accept the prompt-injection risk as inherent, but mitigate it with multiple **defense-in-depth** layers:

### 1. Consent Gate

`install_skill` is registered with `RequiresConsent: true`. The user sees the exact repository URL and must explicitly approve before the clone proceeds. Installation is a deliberate act, not a background operation.

### 2. Source Tagging and Removal Protection

All skills loaded from `.skills/` are tagged `source: "skills.sh"` in the cache. The `remove_skill` tool enforces a **source gate** — only `skills.sh`-source skills can be removed; local `docs/skills/` skills (curated by the Dobby template authors) are protected from accidental deletion. This ensures the curated skill set cannot be corrupted by the removal path.

### 3. Read-Only Injection

Skill content is injected as system prompt text, not as executable instructions. The model interprets skill content as guidance — a set of patterns, conventions, and best practices — not as imperative commands. The `SkillSelector` frames skills as reference material in the system prompt.

### 4. Demand-Driven Loading

Skills are selected by keyword matching against the user's task description. A skill is never loaded unless the user's prompt contains relevant terms. A malicious skill with no topical overlap is never injected.

### What We Do NOT Do

- **Sandboxed execution:** Skills are not executed in a sandbox — they are text injected into context. There is no code execution path for skill content.
- **Content scanning:** We do not scan skill content for malicious patterns or suspicious instructions. The consent gate, not content analysis, is the primary defense.
- **Publisher allowlisting:** We do not maintain an allowlist of trusted publishers. The ecosystem is open; trust is delegated to the user's judgment at install time.

## Consequences

### Positive

- Users have agency over which third-party skills enter their LLM context — no silent or automatic installations.
- The source gate on `remove_skill` protects curated `docs/skills/` content from accidental deletion.
- The defense layers are independent: bypassing one (e.g., a user approving a malicious skill) does not disable the others (source tagging, demand-driven loading).

### Negative / Effort

- Users must exercise judgment when installing skills from unknown publishers — similar to `npm install` or `pip install`. This places a cognitive burden on the user that allowlisting or scanning would reduce.
- A compromised or malicious skill that passes the consent gate could influence model behavior in ways that are difficult to detect or audit. This is an accepted residual risk, documented here so that future maintainers do not inadvertently weaken the consent gate (e.g., by adding silent auto-install or removing the `RequiresConsent` flag).
- The security model relies on the user understanding the prompt-injection risk. The consent prompt should be explicit about this — future work should ensure the confirmation message clearly communicates the risk, not just the repository URL.
