# Review by Roles Checklist

This checklist guides the quality-gate process for merging any source branch into a target branch using specialized reviewer roles.

## Starting the SOP with Assistant

To initiate the review‑by‑roles process using the Assistant, provide a prompt that specifies the source branch, target branch, and the location of your role configuration files.

**Example prompt with individual configuration paths:**

```
Execute docs/steps/review-by-roles.md

source branch: revise-steps
target branch: dev

ARCHITECT_CONFIG: /path/to/your/architect.yaml
TESTER_CONFIG: /path/to/your/tester.yaml
REVIEWER_CONFIG: /path/to/your/reviewer.yaml
CODER_CONFIG: /path/to/your/coder.yaml
ASSISTANT_CONFIG: /path/to/your/assistant.yaml
```

The Assistant will set the corresponding environment variables (ARCHITECT_CONFIG, TESTER_CONFIG, etc.) based on the values provided.

**Alternatively, using a common configuration directory:**

```
Execute docs/steps/review-by-roles.md

source branch: revise-steps
target branch: dev

CONFIG_DIR: /path/to/your/configs
```

If you provide `CONFIG_DIR`, the Assistant will expect the default file names (`architect.yaml`, `tester.yaml`, etc.) in that directory.

## Configuration Files

Role-specific configuration files can be provided in two ways:

### Option 1: Individual Configuration Paths (Recommended)

Set environment variables for each role pointing to the respective configuration file:

- **Architect**: `${ARCHITECT_CONFIG}` (e.g., `/path/to/architect.yaml`)
- **Tester**: `${TESTER_CONFIG}` (e.g., `/path/to/tester.yaml`)
- **Reviewer**: `${REVIEWER_CONFIG}` (e.g., `/path/to/reviewer.yaml`)
- **Coder**: `${CODER_CONFIG}` (e.g., `/path/to/coder.yaml`)
- **Assistant**: `${ASSISTANT_CONFIG}` (e.g., `/path/to/assistant.yaml`)

**Example**: 
```bash
export ARCHITECT_CONFIG=/home/user/project/configs/architect.yaml
export TESTER_CONFIG=/home/user/project/configs/tester.yaml
```

### Option 2: Common Configuration Directory with Default File Names

Set the `CONFIG_DIR` environment variable to point to a directory containing the configuration files with the following **default** names:

- **Architect**: `${CONFIG_DIR}/architect.yaml`
- **Tester**: `${CONFIG_DIR}/tester.yaml`
- **Reviewer**: `${CONFIG_DIR}/reviewer.yaml`
- **Coder**: `${CONFIG_DIR}/coder.yaml`
- **Assistant**: `${CONFIG_DIR}/assistant.yaml`

**Example**: `export CONFIG_DIR=/path/to/your/configs`

**Note**: If you don't set any environment variables, the tool will look for configuration files in the `configs/` directory relative to the project root by default. In that case, replace `${CONFIG_DIR}/` with `configs/` in the commands below.

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
- If using individual configuration paths, ensure each `*_CONFIG` environment variable points to a valid configuration file. If using `CONFIG_DIR`, ensure it points to the directory containing role configuration files. The default location is the `configs/` directory relative to the project root.
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
tell-me-go -new -r -c ${ARCHITECT_CONFIG} &> /dev/null
```

**Note:** Replace `<source-branch>` and `<target-branch>` with the actual branch names (e.g., `feature/xyz` and `main`). The `&> /dev/null` discards stdout and stderr. If you need to debug, redirect to a log file instead (e.g., `> /tmp/architect-review.log 2>&1`).

If you are using `CONFIG_DIR` instead of individual paths, replace `${ARCHITECT_CONFIG}` with `${CONFIG_DIR}/architect.yaml`, `${TESTER_CONFIG}` with `${CONFIG_DIR}/tester.yaml`, and `${REVIEWER_CONFIG}` with `${CONFIG_DIR}/reviewer.yaml`.

Repeat for tester and reviewer by replacing the config path accordingly (use ${TESTER_CONFIG} for tester, ${REVIEWER_CONFIG} for reviewer).

## Step 2 – Collect Responses

After each review request, retrieve the responses:

```bash
# Retrieve the last 3 responses from the Architect review
tell-me-go -l 3 -r -c ${ARCHITECT_CONFIG}
```

The `-l 3` flag limits output to the last 3 messages. Adjust the number as needed.

If you are using `CONFIG_DIR` instead of individual paths, replace `${ARCHITECT_CONFIG}` with `${CONFIG_DIR}/architect.yaml` (and similarly for tester and reviewer).

Again, repeat for tester and reviewer (using ${TESTER_CONFIG} and ${REVIEWER_CONFIG}, or ${CONFIG_DIR}/tester.yaml and ${CONFIG_DIR}/reviewer.yaml if using CONFIG_DIR).

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