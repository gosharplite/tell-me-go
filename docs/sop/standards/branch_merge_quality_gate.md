# Standard Operating Procedure: Source Branch Quality Gate for Merge

## Purpose
To ensure that a source branch meets quality standards across architecture, code, testing, and security before being merged into a target branch (e.g., `main`, `master`). This SOP uses the `tell-me-go` multi‑provider reasoning agent with an **Assistant** role orchestrating four specialized reviewer roles: **Architect**, **Coder**, **Tester**, and **Reviewer**.

## Scope
Applies to all feature, bug‑fix, and refactoring branches that are candidates for merging into a protected target branch.

## Roles & Responsibilities
| Role | Configuration File | Responsibility | **Prohibited Actions** |
|------|-------------------|----------------|------------------------|
| **Assistant** | `assistant.yaml` | Orchestrates the quality‑gate process: invokes specialist roles (Architect, Coder, Tester, Reviewer), coordinates fixes, aggregates findings, and presents summary for human approval. | **MUST NOT perform technical analysis, code review, architectural assessment, or testing evaluation directly.** The Assistant's only technical tasks are administrative checks in Step 2 (CI status, merge conflicts, formatting, static analysis, test suite, coverage). All substantive technical review MUST be delegated to the appropriate specialist role. |
| **Architect** | `architect.yaml` | Assess architectural integrity, dependency boundaries, scalability, and adherence to established patterns. | Must not modify files; provides instructions to Coder. |
| **Coder** | `coder.yaml` | Implement changes, write tests, enforce coding standards (the only role with write access). | Must only modify files when explicitly instructed by Architect/Tester/Reviewer findings. |
| **Tester** | `tester.yaml` | Evaluate test coverage, edge‑case handling, flaky‑test risks, and testing strategy. | Must not modify files; provides instructions to Coder. |
| **Reviewer** | `reviewer.yaml` | Review code for idiomatic Go, security vulnerabilities, concurrency issues, and error‑handling correctness. | Must not modify files; provides instructions to Coder. |

**Workflow:** Human starts the Assistant role; the Assistant coordinates the other four roles by invoking `tell-me-go` with their respective configuration files.

**Role Boundary Enforcement Principle:**  
The multi‑agent workflow enforces strict separation of concerns. The **Assistant is a pure orchestrator** – it MUST NOT perform substantive technical analysis, architectural review, code review, or testing evaluation. These tasks MUST be delegated to the appropriate specialist role (Architect, Reviewer, Tester). Violation of this boundary constitutes a workflow failure and requires restarting the quality‑gate process from Step 1.

## Prerequisites
1. `tell-me-go` installed and available in `PATH`.
2. All five role configuration files available at the default location: `/Users/johndoe/tmp/dualnets/seed/notebooks/mbp-johndoe/ait/yaml/`.
3. The source branch is fully rebased/synced with the target branch and passes all automated CI checks (build, unit tests, lint, security scan).
4. The working directory is the root of the Go module to be reviewed.
5. All `tell-me-go` invocations must pipe prompts via stdin (e.g., `echo "prompt" | tell-me-go ...`) to prevent the `ask_user` tool from hanging.

## Procedure

**Important:** When invoking `tell-me-go` for any role, always pipe the prompt via stdin (e.g., `echo "prompt" | tell-me-go ...`) to prevent the `ask_user` tool from hanging.

### Step 1 – Human Initiates Assistant
The human starts the Assistant role with a command that triggers the quality‑gate workflow:

```bash
echo "Execute the branch‑merge quality‑gate SOP for branch $(git branch --show-current). Coordinate the Architect, Coder, Tester, and Reviewer roles to ensure the branch is ready to merge into main. Provide me with a final summary before proceeding with the merge." | \
tell-me-go -new -c /Users/johndoe/tmp/dualnets/seed/notebooks/mbp-johndoe/ait/yaml/assistant.yaml
```

**Note:** The `-new` flag ensures the Assistant starts a fresh conversation, preventing any residual context from interfering with the quality‑gate assessment.

The Assistant will then perform the following steps autonomously.

### Step 2 – Automated Checks (Assistant)
**Boundary Note:** These are **administrative/pre‑flight checks only** – verifying that automated tooling passes. The Assistant MUST NOT perform substantive technical analysis, architectural assessment, code review, or testing evaluation. Those tasks MUST be delegated to the specialist roles (Architect, Reviewer, Tester).

The Assistant verifies the branch satisfies the following prerequisites:
- [ ] **CI Pipeline** passes (green status).
- [ ] **No merge conflicts** with the target branch.
- [ ] **Code formatting** (`gofmt`, `goimports`) applied.
- [ ] **Static analysis** (`go vet`, `staticcheck`) reports no errors.
- [ ] **Test suite** passes with race detector (`go test -race ./...`).
- [ ] **Coverage** meets the project minimum (e.g., ≥80% with `go test -cover ./...`).

If any check fails, the Assistant reports the issue and stops the workflow (or attempts to fix it via the Coder role if appropriate).

### Step 3 – Architectural Review (Assistant → Architect)
The Assistant invokes the Architect role with a fresh session:

```bash
echo "Review the changes in branch $(git branch --show-current) for architectural concerns. Focus on: 1. Component dependencies and interface boundaries. 2. Scalability risks (blocking calls, state management). 3. Adherence to Clean/Hexagonal Architecture layers. 4. Any 'God Objects' or circular references. Provide findings categorized as [ARCHITECTURAL BLOCKER], [TECHNICAL DEBT], or [REFACTOR]." | \
tell-me-go -new -r -c /Users/johndoe/tmp/dualnets/seed/notebooks/mbp-johndoe/ait/yaml/architect.yaml
```

**Expected Output:** A list of architectural findings with clear "Current vs. Scalable" examples.  
**Exit Criteria:** No `[ARCHITECTURAL BLOCKER]` items remain.

### Step 4 – Implementation Approval (Assistant → Human)
After receiving architectural findings from the Architect, the Assistant presents them to the human along with a proposed implementation plan. The Assistant requests explicit confirmation using the `ask_user` tool before any code changes are made.

Example prompt:

> **Architectural Findings & Implementation Plan**  
> - [ARCHITECTURAL BLOCKER]: Found God Object in service layer (needs refactoring).  
> - [TECHNICAL DEBT]: Circular dependency between packages A and B.  
> - Proposed fix: Split service into smaller components, introduce interface.  
> - Estimated effort: ~2 hours of coding + testing.  
>   
> **Approve implementation?** (yes/no)

**Human must explicitly approve** before proceeding. If the human responds "yes" (or equivalent), the Assistant proceeds to Step 5. If "no" or if no response is received within a timeout, the workflow stops for further discussion.

*Note:* If the Architect reports no required changes, the Assistant can skip directly to Step 6 (Testing Review).

### Step 5 – Coder Implementation (Assistant → Coder)
**Only executed if human approved in Step 4.** The Assistant instructs the Coder role to implement the changes recommended by the Architect, starting a fresh session:

```bash
echo "Implement the changes recommended by the Architect. Follow TDD: 1. Write failing tests for each new requirement. 2. Write minimal code to pass the tests. 3. Ensure 80%+ test coverage, immutability where possible, and no hard‑coded secrets. 4. Self‑review changes against the success criteria." | \
tell-me-go -new -r -c /Users/johndoe/tmp/dualnets/seed/notebooks/mbp-johndoe/ait/yaml/coder.yaml
```

**Note:** The Coder is the only role that can modify files. All other roles provide read‑only guidance.

### Step 6 – Testing Review (Assistant → Tester)
The Assistant invokes the Tester role with a fresh session to validate test quality and coverage:

```bash
echo "Evaluate the test suite for branch $(git branch --show-current). Focus on: 1. Testability of the code (dependency injection, observability). 2. Missing edge‑cases and boundary conditions. 3. Risk of race conditions or non‑deterministic behavior. 4. Adherence to idiomatic Go testing patterns (table‑driven tests, t.Cleanup, t.TempDir). Categorize findings as [TEST BLOCKER], [LOW COVERAGE], or [FLAKY RISK]." | \
tell-me-go -new -r -c /Users/johndoe/tmp/dualnets/seed/notebooks/mbp-johndoe/ait/yaml/tester.yaml
```

**Expected Output:** Specific, actionable instructions for improving test coverage and robustness.  
**Exit Criteria:** No `[TEST BLOCKER]` items remain; coverage meets project minimum.

### Step 7 – Code Review (Assistant → Reviewer)
The Assistant invokes the Reviewer role with a fresh session for idiomatic Go, security, and concurrency checks:

```bash
echo "Perform a detailed code review of branch $(git branch --show-current). Focus on: 1. Security: injections, path traversal, exposed secrets. 2. Idiomatic Go: early returns, proper error wrapping (%w), context propagation. 3. Concurrency: goroutine leaks, race conditions, improper mutex usage. 4. Run diagnostic commands (go vet, staticcheck) if not already done. Categorize findings by priority: [CRITICAL], [HIGH], [MEDIUM]." | \
tell-me-go -new -r -c /Users/johndoe/tmp/dualnets/seed/notebooks/mbp-johndoe/ait/yaml/reviewer.yaml
```

**Expected Output:** A prioritized list of issues with "Bad vs. Good" code examples.  
**Exit Criteria:** No `[CRITICAL]` or `[HIGH]` items remain.

### Step 8 – Feedback Integration (Assistant)
1. Collect all findings from Architect, Tester, and Reviewer.
2. For each finding, decide whether to:
   - **Fix now** – instruct the Coder role to address it.
   - **Defer** – document as technical debt (requires team approval).
   - **Reject** – provide justification in the merge request.
3. Re‑run steps 3–7 after fixes to verify resolution.

### Step 9 – Final Verification (Assistant)
Before requesting human approval, the Assistant confirms:
- [ ] All automated checks (Step 2) still pass.
- [ ] No open architectural, testing, or review blockers.
- [ ] Branch is rebased onto the latest target branch.
- [ ] Commit history is clean (squashed/logical commits).

### Step 10 – Human Approval (Pre‑Merge)
The Assistant presents a concise summary to the human, including:
- List of issues found and resolved.
- Current test coverage percentage.
- Remaining technical‑debt items (if any).
- Any potential risks.

The Assistant then requests explicit confirmation using the `ask_user` tool (or equivalent interactive prompt). Example prompt:

> **Branch‑Ready Summary**  
> - Architecture: No blockers.  
> - Tests: 92% coverage, no flaky risks.  
> - Code Review: All CRITICAL/HIGH issues resolved.  
> - Remaining debt: 2 MEDIUM items deferred (tracked in Jira).  
>  
> **Approve merge?** (yes/no)

**Human must explicitly approve** the merge. If the human responds "yes" (or equivalent), the Assistant proceeds to Step 11. If "no" or if no response is received within a timeout, the workflow stops and the branch remains unmerged.

*Note:* In non‑interactive environments (CI/CD), approval can be bypassed by setting an environment variable (e.g., `AUTO_APPROVE=1`) or by pre‑approving via a command‑line flag.

### Step 11 – Merge (Assistant)
Once approved, the Assistant merges the source branch using the project's preferred strategy (fast‑forward, squash, or merge commit). The merge is tracked (e.g., via GitLab/GitHub merge request, Jira ticket closure).

## Troubleshooting

| Issue | Possible Cause | Resolution |
|-------|----------------|------------|
| `tell-me-go` cannot read configuration file | Path incorrect or missing read permissions | Verify the YAML files exist and are readable. Use `register_readpath` if running inside a sandboxed environment. |
| Assistant cannot invoke other roles | Missing `execute_command` permission or path not authorized | Ensure the Assistant role has `execute_command` tool enabled and the target `tell-me-go` binary is in `PATH`. Use `register_safepath` for the directory containing the role YAMLs. |
| Architect/Tester/Reviewer report no findings | The specialist agent may not have sufficient context about the changes | Provide a diff of the branch with `git diff main...HEAD` as part of the prompt. The Assistant can include the diff in the invocation. |
| Coder fails to write files | The agent lacks write permissions | Ensure the working directory is authorized for writing (`register_safepath`). The Assistant can grant access before invoking the Coder. |
| Coverage does not meet threshold | Tests missing for new or changed code | Use `go test -coverprofile=coverage.out ./...` and `go tool cover -func=coverage.out` to identify uncovered lines. The Assistant can instruct the Coder to add missing tests. |
| Human approval step hangs | The `ask_user` tool may be waiting for input on a non‑interactive terminal | Ensure the Assistant is running in an interactive session. If running in CI/CD, pre‑approve via a flag or environment variable. Additionally, always pipe prompts via stdin (e.g., `echo "prompt" | tell-me-go ...`) to avoid hanging. |
| Merge fails due to permissions | The Assistant lacks Git push/merge rights | Verify Git credentials are configured and the Assistant has the necessary permissions on the repository. |
| Assistant performs technical analysis directly | Assistant violated role boundaries by executing specialist tasks (architectural review, code review, testing evaluation) instead of delegating | **Workflow failure** – Stop immediately and restart from Step 1. Update Assistant configuration to enforce orchestrator‑only behavior. The Assistant MUST use `execute_command` to invoke specialist roles, not perform their work directly. |
| Role session retains unwanted context | Missing `-new` flag when invoking a role | Always include `-new` flag when the Assistant invokes a specialist role to ensure a fresh conversation without residual context. |

## Appendix

### Orchestration Mechanism
The Assistant coordinates the specialist roles by using the `execute_command` tool to run `tell-me-go` with the appropriate configuration file and prompt. Each invocation **must** include the `-new` flag to guarantee a clean session and the `-r` flag to obtain raw output (no markdown formatting) for easier parsing. Prompts must be piped via stdin (e.g., `echo "prompt" | tell-me-go ...`) to ensure the `ask_user` tool does not hang. Example:

```bash
# Simulated internal command executed by the Assistant
echo "Review the changes..." | tell-me-go -new -r -c /path/to/architect.yaml
```

The Assistant maintains state across invocations, aggregating findings and determining when to proceed to the next step.

### Example Multi‑Agent Workflow Script
A bash script that automates the above steps can be placed in the project's `scripts/` directory. See `../../user/piped-multi-agent-workflow.md` for advanced piping patterns.

### Configuration File Locations
- Default role configurations: `/Users/johndoe/tmp/dualnets/seed/notebooks/mbp-johndoe/ait/yaml/`
- Project‑specific overrides: `configs/` (relative to the tell‑me‑go binary)

### References
- [tell‑me‑go README](../../README.md)
- [Go Testing Patterns](../../skills/golang-testing/SKILL.md)
- [Go Development Patterns](../../skills/golang-patterns/SKILL.md)
- [Kubernetes Operations](../../skills/k8s-operations/SKILL.md)

---

**Revision History**
| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | $(date +%Y‑%m‑%d) | AI Assistant | Initial SOP (manual role invocation) |
| 1.1 | $(date +%Y‑%m‑%d) | AI Assistant | Revised for Assistant‑orchestrated workflow with human approval gate |
| 1.2 | $(date +%Y‑%m‑%d) | AI Assistant | Added implementation approval step before Coder begins coding |
| 1.3 | $(date +%Y‑%m‑%d) | AI Assistant | Added `-new` flag to all role invocations to start fresh conversations |
| 1.4 | $(date +%Y‑%m‑%d) | AI Assistant | Added `-r` flag to all role invocations for raw output |
| 1.5 | $(date +%Y‑%m‑%d) | AI Assistant | Added explicit Assistant prohibitions and clarified role boundaries: 1) Assistant MUST NOT perform technical analysis; 2) Defined clear separation of concerns between orchestrator (Assistant) and specialist roles |
| 1.6 | $(date +%Y‑%m‑%d) | AI Assistant | Updated all role invocations to pipe prompts via stdin (e.g., `echo "prompt" | tell-me-go ...`) to prevent the `ask_user` tool from hanging |

