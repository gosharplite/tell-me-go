# Standard Operating Procedure (SOP): Git Workflow (Commit and Push)

### Objective
To ensure that all changes to the `tell-me-go` repository are verified, logically grouped, and documented with clear, descriptive commit messages, adhering to idiomatic Go development practices.

---

### Prerequisites
- Go toolchain (`go`) 1.21+ installed.
- All functional changes must be verified via the Go testing suite.
- No sensitive information (API keys, secrets) should be staged.

---

### Step-by-Step Instructions

#### 1. Pre-Commit Verification
Before staging any files, you must run the following sequence to ensure code integrity:

1.  **Tidy**: Synchronize `go.mod` and `go.sum` with the source code.
    ```bash
    go mod tidy
    ```
2.  **Format**: Ensure code style consistency using the standard Go formatter.
    ```bash
    go fmt ./...
    ```
3.  **Dependency Check**: Ensure no local `replace` directives are staged in `go.mod`.
    ```bash
    grep "replace" go.mod && echo "ERROR: Remove local replacements before commit!" || echo "OK"
    ```
4.  **Lint**: Check for common mistakes and heuristic errors.
    ```bash
    go vet ./...
    # Recommended: golangci-lint run
    ```
4.  **Test**: Run the full test suite with the race detector enabled.
    ```bash
    go test -race ./...
    ```
5.  **Build**: Confirm the project compiles successfully.
    ```bash
    go build ./...
    ```

Ensure all steps pass. **Do NOT commit if any step fails.**

#### 2. Staging Changes
- Stage files logically: `git add <files>`.
- Avoid `git add .` to prevent staging experimental or unrelated files.
- Group related changes (e.g., a logic fix and its corresponding unit test) into a single commit.

#### 3. Composing the Commit Message
Follow the "Summary + Details" format:
- **Summary Line**: A concise (max 50 chars) description starting with a type prefix:
    - `Feat:` (New feature)
    - `Fix:` (Bug fix)
    - `Refactor:` (Code restructuring)
    - `Docs:` (Documentation)
    - `Test:` (Adding/fixing tests)
    - `Chore:` (Maintenance/SOP updates, including `go mod tidy` updates)
- **Detailed Body**: A blank line followed by bullet points explaining the "what" and "why."

#### 4. Final Review and Push
- Review the staged changes: `git diff --staged`.
- Commit: `git commit -m "Summary line..."`.
- Push: `git push origin $(git branch --show-current) --tags` (always include tags if they were created).

---

### ⚠️ AI Agent Implementation Guide
When an AI assistant performs a commit, it must:
1.  **Explicitly verify** `go mod tidy`, `go fmt`, `go vet`, and `go test` status before recommending a commit.
2.  **Generate a structured message** based on the specific packages (e.g., `pkg/core`, `internal/api`) modified.
3.  **Confirm the branch name** using `git branch --show-current`.

---

### Code Templates

#### Recommended Verification Chain:
```bash
go mod tidy && go fmt ./... && go vet ./... && go test -race ./... && go build ./...
```

#### Recommended Commit Pattern:
```bash
git add .
git commit -m "Feat: Implement core history management module

- Added HistoryManager struct in pkg/history.
- Implemented JSON serialization for conversation persistence.
- Added unit tests for pruning logic.
- Verified PASS on all tests (including race detector)."
git push origin $(git branch --show-current)
```

---

### Best Practices
- **Atomic Commits**: Each commit should represent a single logical change.
- **Race Detection**: Always use `-race` during testing to catch concurrency bugs early.
- **Dependency Hygiene**: Never commit code that causes `go mod tidy` to produce changes; run it first.
- **Never Push Broken Code**: The main branch should always compile and pass tests.
- **Secret Scanning**: Double-check that no `.json` key files or sensitive configurations are staged.

