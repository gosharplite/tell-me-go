# Reduce High Cyclomatic Complexity

## Starting the SOP with Assistant

To initiate this process using the Assistant, provide a prompt that specifies the source branch and the location of your role configuration files.

**Example prompt:**

```
Execute docs/steps/reduce-cyclomatic-complexity.md

source branch: reduce-complexity

ARCHITECT_CONFIG: /path/to/your/architect.yaml
TESTER_CONFIG: /path/to/your/tester.yaml
REVIEWER_CONFIG: /path/to/your/reviewer.yaml
CODER_CONFIG: /path/to/your/coder.yaml
ASSISTANT_CONFIG: /path/to/your/assistant.yaml
```

The Assistant will set the corresponding environment variables (ARCHITECT_CONFIG, TESTER_CONFIG, etc.) based on the values provided.

## Configuration Files

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

## Role Responsibilities and Workflow

- **Coder**: The only role that executes code and file changes. Detailed instructions are given to Coder via `tell-me-go`.
- **Architect**: Must review and agree on all instructions before they are given to Coder. The Architect ensures architectural integrity before any implementation begins.
- **Tester**: Evaluates test coverage and testing strategy after changes are implemented.
- **Reviewer**: Performs code review for security, idiomatic Go, and correctness.
- **Assistant**: Coordinates the workflow between roles.

**Key Principle**: All instructions given to Coder must be approved by Architect. This ensures architectural decisions are validated before implementation.

## Prerequisites

Before reducing complexity, ensure:
- Create a new branch from the source branch. Work in the new branch, do not alter source branch.
- You are in the project root directory (where `.git/` is located).
- Ensure each `*_CONFIG` environment variable points to a valid configuration file.
- All automated checks pass:
  ```bash
  go mod tidy && go fmt ./... && go vet ./... && go test -race ./... && go build ./...
  ```

## Chat with roles

```bash
# Request action from Architect (output discarded; see note below)
echo "prompt" | \
tell-me-go -new -r -c ${ARCHITECT_CONFIG} &> /dev/null
```

**Note:**
- The `&> /dev/null` discards stdout and stderr. If you need to debug, redirect to a log file instead (e.g., `> /tmp/architect-review.log 2>&1`).
- Use `-new` when you first chat with a role. If you need to chat continuously, don't use `-new`.

```bash
# Retrieve the last 3 responses from the Architect
tell-me-go -l 3 -r -c ${ARCHITECT_CONFIG}
```

The `-l 3` flag limits output to the last 3 messages. Adjust the number as needed.

## Process
- Ask Architect to provide just one opportunitie to reduce High Cyclomatic Complexity.
- Ask Architect for detail instructions on this opportunity.
- Give this detail instructions to Coder.
- Changes must be reviewed by Architect, Tester and Reviewer.
- Reviews by Tester and Reviewer must be agreed by Architect.
- Coder must address reviews by Architect, Tester and Reviewer.  
- Assitant will output a summary and exit agent loop at the end.
