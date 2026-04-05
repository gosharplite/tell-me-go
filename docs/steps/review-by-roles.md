# Review by Roles

This page check if new source branch is good enough to be merged into new target branch using specialized roles.

- **Architect** – Reviews and approves all changes before implementation.
- **Coder** – Executes code and file changes (the only role that writes code).
- **Tester** – Evaluates test coverage and testing strategy.
- **Reviewer** – Performs security, idiomatic‑Go, and correctness reviews.
- **Assistant** – Coordinates the workflow between roles.

**Key Principle**: Every instruction given to the Coder must first be approved by the Architect. This guarantees architectural decisions are validated before any implementation begins.

## Tooling

The workflow is driven by `tell‑me‑go`, a command‑line tool that communicates with each role using configuration files. Each role’s behavior is defined in a YAML configuration.

## Starting the SOP with Assistant

To initiate the process using the Assistant, provide a prompt that specifies the source branch, target branch and the location of your role configuration files.

**Example prompt:**

```
Execute docs/steps/review-by-roles.md

source branch: dev
target branch: main

ARCHITECT_CONFIG: /path/to/your/architect.yaml
TESTER_CONFIG: /path/to/your/tester.yaml
REVIEWER_CONFIG: /path/to/your/reviewer.yaml
CODER_CONFIG: /path/to/your/coder.yaml
ASSISTANT_CONFIG: /path/to/your/assistant.yaml

You are Assistant, do not take over jobs of other roles. Do not try to do technical work by yourself.
```

The Assistant will set the corresponding environment variables (`ARCHITECT_CONFIG`, `TESTER_CONFIG`, etc.) based on the values you provide.

## Configuration Files

Set environment variables for each role pointing to its respective configuration file:

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

## Prerequisites

Before requesting reviews, ensure:

1. **Branch**: Create two new branches from the source branch and target branch.
   ```bash
   # Example setup for source:dev and target:main
   git checkout main && git checkout -b review/main
   git checkout dev && git checkout -b review/dev
   ```
   All changes must be made in the `review/source-branch`; do not alter the original branches directly.
2. **Directory**: You are in the project root directory (where `.git/` is located).
3. **Configuration**: Each `*_CONFIG` environment variable points to a valid YAML configuration file.
4. **Automated checks pass**:
   ```bash
   go mod tidy && go fmt ./... && go vet ./... && go test -race ./... && go build ./...
   ```

## Strict Role Adherence

- **Assistant** MUST NOT interpret diffs, logs, or code logic.
- **Assistant**'s primary responsibility is to relay information using `tell-me-go`.
- If a role requires context (like a `git diff`), the Assistant should provide the raw data without adding technical commentary.

## Chatting with Roles

To request action from a role, use `tell‑me‑go`. The basic pattern is:

```bash
# Request action from Architect (output discarded)
echo "your prompt" | \
tell-me-go -new -r -c ${ARCHITECT_CONFIG} &> /dev/null
```

**Notes:**
- The `&> /dev/null` discards stdout and stderr to **keep token usage low** and keep the terminal clean while the role processes in the background.
- If you need to debug, redirect to a log file instead (e.g., `> /tmp/architect-review.log 2>&1`).
- Use `-new` when you first chat with a role. For a continuous conversation, omit `-new`.

To retrieve the last responses from a role:

```bash
# Retrieve the last 3 responses from the Architect
tell-me-go -l 3 -r -c ${ARCHITECT_CONFIG}
```

Adjust the number `-l 3` as needed.

**Important:** Assistant must verify the prompt content is non‑empty and appropriate for the target role before forwarding it. For large or complex prompts, it is better to use `cat` than `echo` to pipe the prompt to tell-me-go.

## Process (Step‑by‑Step)

1. **Ask Architect** to identify gaps or issues that prevent merging between the new source branch and new target branch.
2. **Ask Architect** for detailed instructions to fix issues for Coder.
3. **Give those detailed instructions** to the Coder.
4. **Review the changes**:
   - Architect must review the implementation.
   - Tester must evaluate test coverage and strategy.
   - Reviewer must perform security, idiomatic‑Go, and correctness review.
5. **Reviews by Tester and Reviewer must be agreed by Architect** before proceeding.
6. **Coder must address** all feedback from Architect, Tester, and Reviewer.
7. **Assistant outputs a summary** for each task, commits the changes to the new source branch, and after all opportunities are addressed, outputs a final summary and exits the agent loop.

Repeat steps 1‑6 for each task until the overall quality meets the project’s standards.
