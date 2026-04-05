# Review by Roles Checklist

This checklist guides the quality-gate process for merging any source branch into a target branch using specialized reviewer roles.

## Starting the SOP with Assistant

To initiate the review‑by‑roles process using the Assistant, provide a prompt that specifies the source branch, target branch, and the location of your role configuration files.

**Example prompt:**

```
Execute docs/steps/review-by-roles.md

source branch: revise-steps
target branch: dev

CONFIG_DIR: /path/to/your/configs
```

The Assistant will then guide you through the checklist, ensuring each role (Architect, Tester, Reviewer) is consulted and that all quality gates are satisfied before merging.

## Configuration Files

Role-specific configuration files must be specified via the `CONFIG_DIR` environment variable. By default, the tool looks for role-specific configuration files in the `configs/` directory (relative to the project root). To use a different location, set the `CONFIG_DIR` environment variable to point to your custom directory.

- **Architect**: `${CONFIG_DIR}/architect.yaml`
- **Tester**: `${CONFIG_DIR}/tester.yaml`
- **Reviewer**: `${CONFIG_DIR}/reviewer.yaml`
- **Coder**: `${CONFIG_DIR}/coder.yaml`
- **Assistant**: `${CONFIG_DIR}/assistant.yaml`

**Example**: `export CONFIG_DIR=/path/to/your/configs`

**Note**: If you don't set `CONFIG_DIR`, the tool will look for configuration files in the `configs/` directory relative to the project root by default. In that case, replace `${CONFIG_DIR}/` with `configs/` in the commands below.

## Role Responsibilities and Workflow

- **Coder**: The only role that executes code and file changes. Detailed instructions are given to Coder via `tell-me-go`.
- **Architect**: Must review and agree on all instructions before they are given to Coder. The Architect ensures architectural integrity before any implementation begins.
- **Tester**: Evaluates test coverage and testing strategy after changes are implemented.
- **Reviewer**: Performs code review for security, idiomatic Go, and correctness.
- **Assistant**: Coordinates the workflow between roles (when used).

**Key Principle**: All instructions given to Coder must be approved by Architect. This ensures architectural decisions are validated before implementation.

## Prerequisites

Before requesting reviews, ensure:
- You are in the project root directory (where `.git/` is located).
- If using the CONFIG_DIR environment variable, ensure it points to the directory containing role configuration files. The default location is the `configs/` directory relative to the project root.
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
echo "Is the difference between source branch '<source-branch>' and target branch '<target-branch>' sufficient for merging?" | \
tell-me-go -new -r -c ${CONFIG_DIR}/architect.yaml &> /dev/null
```

**Note:** Replace `<source-branch>` and `<target-branch>` with the actual branch names (e.g., `feature/xyz` and `main`). The `&> /dev/null` discards stdout and stderr. If you need to debug, redirect to a log file instead (e.g., `> /tmp/architect-review.log 2>&1`).

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

If any role identifies blocking issues (marked as **[HIGH]** or **[ARCHITECTURAL BLOCKER]**), address them before proceeding. **Important**: Any required code changes must be implemented by the **Coder** role using instructions approved by the **Architect**.

Non‑blocking findings (e.g., documentation improvements) should be addressed either before merging or scheduled as follow‑up tasks.

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
