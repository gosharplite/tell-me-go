# Standard Operating Procedure: Source Branch Quality Gate for Merge

## Purpose
To ensure that a source branch meets quality standards across architecture, code, testing, and security before being merged into a target branch (e.g., `main`, `master`). This SOP uses the `tell-me-go` multi‑provider reasoning agent with an **Assistant** role orchestrating four specialized reviewer roles: **Architect**, **Coder**, **Tester**, and **Reviewer**.

## Scope
Applies to all feature, bug‑fix, and refactoring branches that are candidates for merging into a protected target branch.

## Roles & Responsibilities
| Role | Configuration File | Responsibility |
|------|-------------------|----------------|
| **Assistant** | `assistant.yaml` | Orchestrates the quality‑gate process, invokes specialist roles, coordinates fixes, and presents summary for human approval. |
| **Architect** | `architect.yaml` | Assess architectural integrity, dependency boundaries, scalability, and adherence to established patterns. |
| **Coder** | `coder.yaml` | Implement changes, write tests, enforce coding standards (the only role with write access). |
| **Tester** | `tester.yaml` | Evaluate test coverage, edge‑case handling, flaky‑test risks, and testing strategy. |
| **Reviewer** | `reviewer.yaml` | Review code for idiomatic Go, security vulnerabilities, concurrency issues, and error‑handling correctness. |

**Workflow:** Human starts the Assistant role; the Assistant coordinates the other four roles by invoking `tell-me-go` with their respective configuration files.

## Prerequisites
1. `tell-me-go` installed and available in `PATH`.
2. All five role configuration files available at the default location: `/Users/johndoe/tmp/dualnets/seed/notebooks/mbp-johndoe/ait/yaml/`.
3. The source branch is fully rebased/synced with the target branch and passes all automated CI checks (build, unit tests, lint, security scan).
4. The working directory is the root of the Go module to be reviewed.

## Procedure

### Step 1 – Human Initiates Assistant
The human starts the Assistant role with a command that triggers the quality‑gate workflow:

```bash
tell-me-go -c /Users/johndoe/tmp/dualnets/seed/notebooks/mbp-johndoe/ait/yaml/assistant.yaml \
  "Execute the branch‑merge quality‑gate SOP for branch $(git branch --show-current). \
   Coordinate the Architect, Coder, Tester, and Reviewer roles to ensure the branch is ready to merge into main. \
   Provide me with a final summary before proceeding with the merge."
```

The Assistant will then perform the following steps autonomously.

### Step 2 – Automated Checks (Assistant)
The Assistant verifies the branch satisfies the following prerequisites:
- [ ] **CI Pipeline** passes (green status).
- [ ] **No merge conflicts** with the target branch.
- [ ] **Code formatting** (`gofmt`, `goimports`) applied.
- [ ] **Static analysis** (`go vet`, `staticcheck`) reports no errors.
- [ ] **Test suite** passes with race detector (`go test -race ./...`).
- [ ] **Coverage** meets the project minimum (e.g., ≥80% with `go test -cover ./...`).

If any check fails, the Assistant reports the issue and stops the workflow (or attempts to fix it via the Coder role if appropriate).

### Step 3 – Architectural Review (Assistant → Architect)
The Assistant invokes the Architect role:

```bash
tell-me-go -c /Users/johndoe/tmp/dualnets/seed/notebooks/mbp-johndoe/ait/yaml/architect.yaml \
  "Review the changes in branch $(git branch --show-current) for architectural concerns. \
   Focus on: \
   1. Component dependencies and interface boundaries. \
   2. Scalability risks (blocking calls, state management). \
   3. Adherence to Clean/Hexagonal Architecture layers. \
   4. Any 'God Objects' or circular references. \
   Provide findings categorized as [ARCHITECTURAL BLOCKER], [TECHNICAL DEBT], or [REFACTOR]."
```

**Expected Output:** A list of architectural findings with clear “Current vs. Scalable” examples.  
**Exit Criteria:** No `[ARCHITECTURAL BLOCKER]` items remain.

### Step 4 – Coder Implementation (Assistant → Coder)
If the Architect identifies required changes, the Assistant instructs the Coder role to implement them:

```bash
tell-me-go -c /Users/johndoe/tmp/dualnets/seed/notebooks/mbp-johndoe/ait/yaml/coder.yaml \
  "Implement the changes recommended by the Architect. Follow TDD: \
   1. Write failing tests for each new requirement. \
   2. Write minimal code to pass the tests. \
   3. Ensure 80%+ test coverage, immutability where possible, and no hard‑coded secrets. \
   4. Self‑review changes against the success criteria."
```

**Note:** The Coder is the only role that can modify files. All other roles provide read‑only guidance.

### Step 5 – Testing Review (Assistant → Tester)
The Assistant invokes the Tester role to validate test quality and coverage:

```bash
tell-me-go -c /Users/johndoe/tmp/dualnets/seed/notebooks/mbp-johndoe/ait/yaml/tester.yaml \
  "Evaluate the test suite for branch $(git branch --show-current). \
   Focus on: \
   1. Testability of the code (dependency injection, observability). \
   2. Missing edge‑cases and boundary conditions. \
   3. Risk of race conditions or non‑deterministic behavior. \
   4. Adherence to idiomatic Go testing patterns (table‑driven tests, t.Cleanup, t.TempDir). \
   Categorize findings as [TEST BLOCKER], [LOW COVERAGE], or [FLAKY RISK]."
```

**Expected Output:** Specific, actionable instructions for improving test coverage and robustness.  
**Exit Criteria:** No `[TEST BLOCKER]` items remain; coverage meets project minimum.

### Step 6 – Code Review (Assistant → Reviewer)
The Assistant invokes the Reviewer role for idiomatic Go, security, and concurrency checks:

```bash
tell-me-go -c /Users/johndoe/tmp/dualnets/seed/notebooks/mbp-johndoe/ait/yaml/reviewer.yaml \
  "Perform a detailed code review of branch $(git branch --show-current). \
   Focus on: \
   1. Security: injections, path traversal, exposed secrets. \
   2. Idiomatic Go: early returns, proper error wrapping (%w), context propagation. \
   3. Concurrency: goroutine leaks, race conditions, improper mutex usage. \
   4. Run diagnostic commands (go vet, staticcheck) if not already done. \
   Categorize findings by priority: [CRITICAL], [HIGH], [MEDIUM]."
```

**Expected Output:** A prioritized list of issues with “Bad vs. Good” code examples.  
**Exit Criteria:** No `[CRITICAL]` or `[HIGH]` items remain.

### Step 7 – Feedback Integration (Assistant)
1. Collect all findings from Architect, Tester, and Reviewer.
2. For each finding, decide whether to:
   - **Fix now** – instruct the Coder role to address it.
   - **Defer** – document as technical debt (requires team approval).
   - **Reject** – provide justification in the merge request.
3. Re‑run steps 3–6 after fixes to verify resolution.

### Step 8 – Final Verification (Assistant)
Before requesting human approval, the Assistant confirms:
- [ ] All automated checks (Step 2) still pass.
- [ ] No open architectural, testing, or review blockers.
- [ ] Branch is rebased onto the latest target branch.
- [ ] Commit history is clean (squashed/logical commits).

### Step 9 – Human Approval
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

**Human must explicitly approve** the merge. If the human responds “yes” (or equivalent), the Assistant proceeds to merge. If “no” or if no response is received within a timeout, the workflow stops and the branch remains unmerged.

*Note:* In non‑interactive environments (CI/CD), approval can be bypassed by setting an environment variable (e.g., `AUTO_APPROVE=1`) or by pre‑approving via a command‑line flag.

### Step 10 – Merge (Assistant)
Once approved, the Assistant merges the source branch using the project’s preferred strategy (fast‑forward, squash, or merge commit). The merge is tracked (e.g., via GitLab/GitHub merge request, Jira ticket closure).

## Troubleshooting

| Issue | Possible Cause | Resolution |
|-------|----------------|------------|
| `tell-me-go` cannot read configuration file | Path incorrect or missing read permissions | Verify the YAML files exist and are readable. Use `register_readpath` if running inside a sandboxed environment. |
| Assistant cannot invoke other roles | Missing `execute_command` permission or path not authorized | Ensure the Assistant role has `execute_command` tool enabled and the target `tell-me-go` binary is in `PATH`. Use `register_safepath` for the directory containing the role YAMLs. |
| Architect/Tester/Reviewer report no findings | The specialist agent may not have sufficient context about the changes | Provide a diff of the branch with `git diff main...HEAD` as part of the prompt. The Assistant can include the diff in the invocation. |
| Coder fails to write files | The agent lacks write permissions | Ensure the working directory is authorized for writing (`register_safepath`). The Assistant can grant access before invoking the Coder. |
| Coverage does not meet threshold | Tests missing for new or changed code | Use `go test -coverprofile=coverage.out ./...` and `go tool cover -func=coverage.out` to identify uncovered lines. The Assistant can instruct the Coder to add missing tests. |
| Human approval step hangs | The `ask_user` tool may be waiting for input on a non‑interactive terminal | Ensure the Assistant is running in an interactive session. If running in CI/CD, pre‑approve via a flag or environment variable. |
| Merge fails due to permissions | The Assistant lacks Git push/merge rights | Verify Git credentials are configured and the Assistant has the necessary permissions on the repository. |

## Appendix

### Orchestration Mechanism
The Assistant coordinates the specialist roles by using the `execute_command` tool to run `tell-me-go` with the appropriate configuration file and prompt. Example:

```bash
# Simulated internal command executed by the Assistant
tell-me-go -c /path/to/architect.yaml "Review the changes..."
```

The Assistant maintains state across invocations, aggregating findings and determining when to proceed to the next step.

### Example Multi‑Agent Workflow Script
A bash script that automates the above steps can be placed in the project’s `scripts/` directory. See `../../user/piped-multi-agent-workflow.md` for advanced piping patterns.

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
| 1.0 | $(date +%Y-%m‑%d) | AI Assistant | Initial SOP (manual role invocation) |
| 1.1 | $(date +%Y-%m‑%d) | AI Assistant | Revised for Assistant‑orchestrated workflow with human approval gate |