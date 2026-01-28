<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->


# Standard Operating Procedure (SOP): Public Release Process

### Objective
This SOP defines the requirements and steps for publishing a new public release of the `tell-me-go` project, ensuring code quality, security compliance, and comprehensive documentation for a Go-based application.

---

### Prerequisites
- All tests (Unit, Integration, and E2E) must pass: `go test ./...`.
- `go mod tidy` has been run and `go.sum` is up to date.
- The `README.md` must be updated with the latest features and usage examples.
- A clean Git state (no uncommitted or untracked experimental files).
- Access to the repository with permission to push tags.

---

### Step-by-Step Instructions

#### 0. Task Initialization (⚠️ NEW)
Before starting the release, the agent MUST initialize the project state to prevent "process amnesia" across session boundaries:

1.  **Initialize Tasks**: Use `manage_tasks` to add the following milestones:
    - `add`: "**SOP Compliance: public_release.md**" (Mandatory Anchor Task)
    - `add`: "**Initialize Scratchpad with Granular Checklist**"
    - `add`: "Security & Privacy Audit"
    - `add`: "Functional Verification (Tests/E2E)"
    - `add`: "Versioning & Tagging (Local)"
    - `add`: "Remote Synchronization (Git Push)"
    - `add`: "Final Verification & Cleanup"

2.  **Initialize Scratchpad**: Use `manage_scratchpad` to `write` the full **Release Checklist** (found at the bottom of this document) to the persistent scratchpad. 
    - **CRITICAL**: The agent MUST update this scratchpad checklist after every sub-step to maintain a granular record of progress.

#### 1. Security & Privacy Audit
Before any public release, perform a mandatory security scan:
- **Secret Scanning**: Ensure no Service Account JSON keys (`*.json`), API keys, or `.env` files are in the repository. Use `git grep` for common patterns like `"private_key"` or `"api_key"`.
- **Ignore Check**: Verify `.gitignore` covers `output/`, `*.log`, and any local session files.
- **Privacy**: Ensure no personal data or proprietary internal URLs are hardcoded in the Go source code or YAML configs.

#### 2. Documentation & Compliance Review
- **README Check**: Verify that `README.md` is strictly up-to-date. This includes:
    - New CLI tools (e.g., cost estimation tools).
    - New configuration parameters.
    - Updated feature lists.
    - Updated usage examples.
- **Config Sync Verification**: Ensure the `configs/` folder and its default YAML files are perfectly synchronized with the examples provided in `README.md`.
- **License**: Verify `LICENSE` (MIT) is present in the root.
- **SPDX Headers**: Ensure all Go source files (`*.go`), YAML files, and Markdown files (`*.md`) in the `SOP/` directory contain the standard SPDX-License-Identifier header.
    - **⚠️ CRITICAL**: Modifications to core entry points (in `cmd/`) **MUST** follow the safety procedures in `SOP/lifecycle/self_update_safety.md`.
- **SOP Sync**: Verify that the `SOP/` directory reflects the current project architecture.
- **Version Bump**: Update any version constants in the source code (e.g., in a `version` package or the main CLI help text).

#### 3. Release Guardrails (⚠️ CRITICAL)
To prevent build failures for users (e.g., local file path leaks), perform these checks:
- **No Local Replacements**: Ensure `go.mod` does **NOT** contain `replace` directives pointing to local directories.
    ```bash
    grep "replace" go.mod && echo "ERROR: Remove local replacements!" || echo "OK"
    ```
- **Clean Room Verification**: Perform a trial build in a temporary, fresh directory to simulate a user's `git clone`.
    ```bash
    TMP_DIR=$(mktemp -d)
    git clone . $TMP_DIR
    cd $TMP_DIR && go build ./...
    # If this fails, the release is NOT ready.
    ```

#### 4. Final Functional Verification
Run the full suite in the main repository. This MUST include the E2E tests which verify the CLI binary's behavior:
```bash
go mod tidy
go fmt ./...
go vet ./...
go test -race ./...
go build ./...
```
*Note: The E2E suite in `tests/e2e/` is critical for ensuring that multi-turn tool orchestration and security gates are functional in the final binary.*

#### 5. Changelog Update
Create or update a `CHANGELOG.md` or the "Latest Changes" section of the README:
- Categorize changes into: `Added`, `Changed`, `Fixed`, `Removed`.
- Highlight Go-specific improvements (e.g., "Optimized concurrency with worker pools").

#### 6. Git Tagging and Remote Synchronization (⚠️ CRITICAL)
A release is **not complete** until it is reachable by the public on the remote repository:

1.  **Start in dev branch**: Ensure you are on `dev` and all changes for the release are committed.
    ```bash
    git checkout dev
    ```
2.  **Merge dev into main**:
    ```bash
    git checkout main
    git merge dev
    ```
3.  **Tag the release**:
    ```bash
    git tag -a v1.x.x -m "Release version 1.x.x - [Brief summary]"
    ```
4.  **Push Everything**:
    ```bash
    git push origin main dev --tags
    ```
5.  **Return to dev and Bump Version**:
    ```bash
    git checkout dev
    # Immediately bump the version to next cycle with -dev suffix
    # e.g. Update main.go to "1.16.0-dev"
    git add cmd/tell-me-go/main.go
    git commit -m "Chore: Start next development cycle"
    git push origin dev
    ```
6.  **Verify Synchronization**:
    *   **CRITICAL**: Run `git status` to ensure `Your branch is up to date with 'origin/...'`.
    *   Check that `main` matches `origin/main`.
    *   Check that `dev` matches `origin/dev`.
    *   `dev` should now be exactly 1 commit ahead of `main` (the version bump).
7.  **External Verification**: If possible, verify the release from a separate local folder or environment using `git pull`.

---

### Release Checklist
- [ ] Security audit completed (no secrets found).
- [ ] **No `replace` directives** in `go.mod`.
- [ ] **Clean room verification** passed in a fresh clone.
- [ ] **E2E Tests passed**: `go test -v ./tests/e2e` returns **PASS**.
- [ ] `go test ./...` returns **PASS**.
- [ ] `go mod tidy` run and `go.sum` committed.
- [ ] `README.md` includes all new features and configuration options.
- [ ] `SOP/` directory is updated to reflect structural changes.
- [ ] Branch `dev` is successfully merged into `main`.
- [ ] Git tag is applied.
- [ ] **Pushed to Remote**: `git push origin main dev --tags` executed successfully.
- [ ] **Externally Verified**: Checked version from a separate folder or via GitHub UI.

---

### Best Practices
- **Anchor Tasking**: Always include a task that explicitly names the SOP being followed (e.g., "SOP Compliance: public_release.md"). This ensures that if a new session starts, listing the tasks immediately identifies the governing procedure.
- **Task Manager vs. Scratchpad**: The agent MUST use `manage_tasks` for high-level process milestones and the `scratchpad` for granular, low-level checkboxes (like specific file audits). This dual-layer approach prevents "process amnesia" and ensures critical steps like remote synchronization are never missed.
- **Verification of Remote State**: Never announce completion until `git status` or `git remote -v` confirms the local state is strictly synchronized with `origin`. The "Remote Synchronization" task must only be marked completed AFTER the push is verified.
- **Strict Checklist Adherence**: Items in the scratchpad should be marked only after verified completion.
- **Release Often**: Small, frequent releases are easier to audit and test.
- **Binary Verification**: If distributing binaries, verify they run on target architectures (Linux/macOS/Windows).
- **Draft Releases**: Use GitHub's "Draft Release" feature to stage the release notes.
- **Minimal Dependencies**: Maintain the project's philosophy by preferring the Go standard library and avoiding unnecessary external packages.
