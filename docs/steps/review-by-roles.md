# Review by Roles Checklist

This checklist guides the quality-gate process for merging any source branch into a target branch using specialized reviewer roles.

## Configuration Files

Role-specific configuration files are located in the `configs/` directory (relative to project root). You can also use absolute paths if you have custom configurations elsewhere.

- **Architect**: `${CONFIG_DIR}/architect.yaml` (default: `configs/architect.yaml`)
- **Tester**: `${CONFIG_DIR}/tester.yaml` (default: `configs/tester.yaml`)
- **Reviewer**: `${CONFIG_DIR}/reviewer.yaml` (default: `configs/reviewer.yaml`)
- **Coder**: `${CONFIG_DIR}/coder.yaml` (default: `configs/coder.yaml`)
- **Assistant**: `${CONFIG_DIR}/assistant.yaml` (default: `configs/assistant.yaml`)

Set `CONFIG_DIR` environment variable to override the default location.

## Prerequisites

Before requesting reviews, ensure:
- The source branch is fully rebased onto the latest target branch.
- All automated checks pass:
  ```bash
  go mod tidy && go fmt ./... && go vet ./... && go test -race ./... && go build ./...
  ```
- No merge conflicts exist.

## Step 1 – Request Reviews

Use the following prompt for each reviewer role:

```bash
# Request review from Architect (output discarded; see note below)
echo "Is the difference between source branch and target branch sufficient for merging?" | \
tell-me-go -new -r -c ${CONFIG_DIR}/architect.yaml &> /dev/null
```

**Note:** The `&> /dev/null` discards stdout and stderr. If you need to debug, redirect to a log file instead (e.g., `> /tmp/architect-review.log 2>&1`).

Repeat for tester and reviewer by replacing the config path accordingly.

## Step 2 – Collect Responses

After each review request, retrieve the responses:

```bash
# Retrieve the last 3 responses from the Architect review
tell-me-go -l 3 -r -c ${CONFIG_DIR}/architect.yaml
```

The `-l 3` flag limits output to the last 3 messages. Adjust the number as needed.

Again, repeat for tester and reviewer.

## Step 3 – Evaluate Reviews

Review the outputs from each role:

- **Architect**: Assesses architectural integrity, dependency boundaries, scalability, and adherence to patterns.
- **Tester**: Evaluates test coverage, edge‑case handling, flaky‑test risks, and testing strategy.
- **Reviewer**: Reviews code for idiomatic Go, security vulnerabilities, concurrency issues, and error‑handling correctness.

If any role identifies blocking issues (marked as **[HIGH]** or **[ARCHITECTURAL BLOCKER]**), address them before proceeding. Non‑blocking findings (e.g., documentation improvements) should be addressed either before merging or scheduled as follow‑up tasks.

## Step 4 – Merge Approval

Once all three reviews are satisfactory (no blocking issues), you may proceed with merging the source branch into the target branch:

```bash
git checkout <target-branch>
git merge <source-branch>
git push origin <target-branch>
```

Replace `<target-branch>` and `<source-branch>` with the actual branch names (e.g., `main` and `feature/xyz`).

## Related Documentation

- **Git Workflow SOP**: `docs/sop/standards/git_workflow.md`
- **Piped Multi‑Agent Workflow**: `docs/user/piped-multi-agent-workflow.md`
- **Original Quality‑Gate SOP (archived)**: `docs/sop/standards/branch_merge_quality_gate.md` (deleted; refer to the above documents for the current process.)

## Changelog

- **2025‑01‑15**: Simplified the original 209‑line SOP into this checklist while preserving essential steps and referencing existing documentation. Fixed hard‑coded paths and grammatical errors.
- **2025‑01‑15**: Added clarifying comments about `-l 3` flag and output redirection.
- **2025‑01‑15**: Renamed from `dev-to-main.md` to `review-by-roles.md` and generalized content to use source/target branch terminology.

---

*This document is intended as a quick reference. For detailed procedures, consult the linked SOPs.*