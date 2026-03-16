<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# Standard Operating Procedure (SOP): Public Release Process

### Objective
This SOP defines the automated workflow for publishing a new public release of the `tell-me-go` project.

**⚠️ SECURITY NOTE**: This SOP MUST be executed with `bypass_confirmation` enabled to allow for automated checks.

---

### Step-by-Step Instructions

#### 1. Task Initialization
1.  **Clear Workspace State**: Use `manage_tasks` (action: clear) and `manage_scratchpad` (action: clear) to ensure a fresh environment.
2.  **Check Cleanliness**: Run `git status`. Ensure the working directory is clean.
3.  **Enable Automation**: Execute `bypass_confirmation`.
4.  **Initialize Milestone**: Use `manage_tasks` to add: `"Public Release vX.Y.Z Readiness"`.
5.  **Sync Workspace**: Ensure you are on the `dev` branch and synchronized with remote: `git fetch origin && git checkout dev && git pull origin dev`.
6.  **Confirm Target Version**: Use `git tag -l` to check the last release version. Use `ask_user` to present the last version and propose the target release version (e.g., `1.1.0`). Store this in the `manage_scratchpad`.

#### 2. Automated Readiness Verification
Run the following tool to perform a comprehensive security, dependency, and functional audit:
```bash
verify_release_readiness
```
**CRITICAL**: All checks in the generated report MUST return **[OK]**. If any check returns **[FAIL]**, you MUST fix the issue before proceeding.

#### 3. Preparation
1.  **Note**: This project relies on Git tags as the single source of truth for versioning. The version is injected at build time using Go linker flags (`ldflags`).
2.  Run `go mod tidy`.
3.  Ensure your code is thoroughly tested.

#### 4. Git Tagging and Remote Synchronization
1.  **Sync and Merge into main**:
    ```bash
    git checkout main
    git fetch origin
    git reset --hard origin/main  # Safety: Ensure main matches remote truth
    git merge dev --no-ff -m "Release version v1.1.0" # Avoid interactive editor pop-up
    ```
2.  **Tag the release**:
    ```bash
    git tag -a v1.1.0 -m "Release version 1.1.0"
    ```
3.  **Build and Verify Binary**: 
    Inject the version flag manually or via `make`:
    ```bash
    go build -ldflags="-X 'main.version=1.1.0'" -o tell-me-go ./cmd/tell-me-go
    # OR
    make build VERSION=1.1.0
    ```
    Verify the binary reports the correct version: `./tell-me-go version` (if a version command exists).
4.  **Push Everything**:
    ```bash
    git push origin main dev --tags
    ```
5.  **Return to dev**:
    ```bash
    git checkout dev
    ```

#### 5. Cleanup and Security Restoration
1.  **Verify Sync**: Run `git status` to ensure all branches are clean and synced.
2.  **Finalize Task**: Use `manage_tasks` (action: update) to mark the release task as `completed`.
3.  **Final Cleanup**: Execute `manage_scratchpad` (action: clear) and `manage_tasks` (action: clear) to leave a clean environment for the next session.
