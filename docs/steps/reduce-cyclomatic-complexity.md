# Reduce High Cyclomatic Complexity

## Purpose

This SOP (Standard Operating Procedure) guides you through reducing high cyclomatic complexity in Go code using a multi‑role workflow. The workflow ensures architectural integrity, test coverage, and code quality by coordinating five distinct roles:

- **Architect** – Reviews and approves all changes before implementation.
- **Coder** – Executes code and file changes (the only role that writes code).
- **Tester** – Evaluates test coverage and testing strategy.
- **Reviewer** – Performs security, idiomatic‑Go, and correctness reviews.
- **Assistant** – Coordinates the workflow between roles.

**Key Principle**: Every instruction given to the Coder must first be approved by the Architect. This guarantees architectural decisions are validated before any implementation begins.

## Tooling

The workflow is driven by `tell‑me‑go`, a command‑line tool that communicates with each role using configuration files. Each role’s behavior is defined in a YAML configuration.

## Starting the SOP with Assistant

To initiate the process using the Assistant, provide a prompt that specifies the source branch and the location of your role configuration files.

**Example prompt:**

```
Execute docs/steps/reduce-cyclomatic-complexity.md

source branch: reduce-complexity

ARCHITECT_CONFIG: /path/to/your/architect.yaml
TESTER_CONFIG: /path/to/your/tester.yaml
REVIEWER_CONFIG: /path/to/your/reviewer.yaml
CODER_CONFIG: /path/to/your/coder.yaml
ASSISTANT_CONFIG: /path/to/your/assistant.yaml

You are Assistant, do not take over jobs of other roles.
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

Before reducing complexity, ensure:

1. **Branch**: Create a new branch from the source branch. All changes must be made in the new branch; do not alter the source branch directly.
2. **Directory**: You are in the project root directory (where `.git/` is located).
3. **Configuration**: Each `*_CONFIG` environment variable points to a valid YAML configuration file.
4. **Automated checks pass**:
   ```bash
   go mod tidy && go fmt ./... && go vet ./... && go test -race ./... && go build ./...
   ```

## Chatting with Roles

To request action from a role, use `tell‑me‑go`. The basic pattern is:

```bash
# Request action from Architect (output discarded; see note below)
echo "your prompt" | \
tell-me-go -new -r -c ${ARCHITECT_CONFIG} &> /dev/null
```

**Notes:**
- The `&> /dev/null` discards stdout and stderr. If you need to debug, redirect to a log file instead (e.g., `> /tmp/architect-review.log 2>&1`).
- Use `-new` when you first chat with a role. For a continuous conversation, omit `-new`.

To retrieve the last responses from a role:

```bash
# Retrieve the last 3 responses from the Architect
tell-me-go -l 3 -r -c ${ARCHITECT_CONFIG}
```

Adjust the number `-l 3` as needed.

**Important:** Assistant must make sure prompt exists before passing to roles.

## Process (Step‑by‑Step)

1. **Ask Architect** to identify **one opportunity** to reduce high cyclomatic complexity.
2. **Ask Architect** for detailed instructions on that opportunity.
3. **Give those detailed instructions** to the Coder.
4. **Review the changes**:
   - Architect must review the implementation.
   - Tester must evaluate test coverage and strategy.
   - Reviewer must perform security, idiomatic‑Go, and correctness review.
5. **Reviews by Tester and Reviewer must be agreed by Architect** before proceeding.
6. **Coder must address** all feedback from Architect, Tester, and Reviewer.
7. **Assistant will output a summary** for each task and commit to the new branch.
8. **Assistant will output a summary** and exit the agent loop at the end.

Repeat steps 1‑7 for each high‑complexity opportunity until the overall complexity meets the project’s standards.

## Example Workflow Snippet

```bash
# 1. Identify an opportunity
echo "Identify one function with high cyclomatic complexity that we can refactor." \
  | tell-me-go -new -r -c ${ARCHITECT_CONFIG} &> /dev/null

# 2. Get detailed instructions
echo "Provide step‑by‑step refactoring instructions for that function." \
  | tell-me-go -r -c ${ARCHITECT_CONFIG} &> /dev/null

# 3. Retrieve the Architect’s instructions
tell-me-go -l 1 -r -c ${ARCHITECT_CONFIG} > /tmp/instructions.txt

# 4. Give instructions to Coder
cat /tmp/instructions.txt | tell-me-go -new -r -c ${CODER_CONFIG} &> /dev/null

# 5. Review (Architect, Tester, Reviewer) …
```

## After the SOP

- Verify that all automated checks still pass.
- Commit your changes to the new branch.
