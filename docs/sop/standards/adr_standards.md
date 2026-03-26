# Standard Operating Procedure (SOP): Architectural Decision Records (ADRs)

### Objective
This SOP defines the process for documenting significant architectural decisions in `tell-me-go` to ensure that the rationale behind design choices is preserved, searchable, and transparent.

---

### 1. When to Create an ADR
An ADR should be created for any decision that has a significant impact on the project's structure, dependencies, or behavior, including:
- Introduction of new core libraries or SDKs.
- Major refactoring of internal package boundaries (e.g., Domain vs. Infrastructure).
- Implementation of new design patterns (e.g., switching to CQRS or a new Adapter style).
- Changes to the security or sandbox model.

### 2. File Location and Naming
- **Directory:** `docs/adr/`
- **Naming Convention:** `YYYY-MM-short-descriptive-title.md` (e.g., `2024-05-multi-provider-strategy.md`).

### 3. Required Format
Every ADR must include the following sections:
- **Title:** Prefixed with `ADR-XXX:`.
- **Status:** (Proposed, Accepted, Rejected, Superseded).
- **Context:** The problem or opportunity that necessitated the decision.
- **Decision:** The chosen course of action and the specific technical approach.
- **Consequences:** Both positive and negative trade-offs resulting from the decision.

### 4. Workflow
1. **Drafting:** The Architect drafts the ADR in a temporary file or discussion.
2. **Review:** The decision is reviewed against existing SOPs (e.g., Security, Testing).
3. **Commitment:** Once accepted, the Coder creates the file in `docs/adr/` and updates any relevant cross-references in the SOPs.
4. **Index Maintenance:** The Coder updates the [ADR index file](../adr/README.md) with the new ADR entry.
5. **Superseding:** If a decision is changed later, the old ADR's status is updated to "Superseded" with a link to the new ADR.

---

### 5. Verification
- Verify that the new ADR is linked in the main `README.md` under a "Design Decisions" section.
- Verify that the [ADR index file](../adr/README.md) has been updated with the new ADR entry.
