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
1.  **Enable Automation**: Execute `bypass_confirmation`.
2.  **Initialize Milestone**: Use `manage_tasks` to add: `"Public Release v1.x.x Readiness"`.
3.  **Sync Workspace**: Ensure you are on the `dev` branch and synchronized with remote: `git fetch origin && git checkout dev && git pull origin dev`.

#### 2. Automated Readiness Verification
Run the following tool to perform a comprehensive security, dependency, and functional audit:
```bash
verify_release_readiness
```
**CRITICAL**: All checks in the generated report MUST return **[OK]**. If any check returns **[FAIL]**, you MUST fix the issue before proceeding.

#### 3. Version Stabilization
1.  Update `Version` in `cmd/tell-me-go/main.go` (remove `-dev` suffix).
2.  Run `go mod tidy`.
3.  **Note**: This project relies on Git history and tags for version tracking. A manual `CHANGELOG.md` is NOT maintained to ensure the Git log remains the single source of truth.
4.  Commit: `git commit -am "Chore: Stabilize version for release v1.x.x"`.

#### 4. Git Tagging and Remote Synchronization
1.  **Merge into main**:
    ```bash
    git checkout main
    git merge dev
    ```
2.  **Tag the release**:
    ```bash
    git tag -a v1.x.x -m "Release version 1.x.x"
    ```
3.  **Push Everything**:
    ```bash
    git push origin main dev --tags
    ```
4.  **Return to dev and Bump Version**:
    ```bash
    git checkout dev
    # Bump version in main.go to next cycle (e.g., "1.66.0-dev")
    git commit -am "Chore: Start next development cycle"
    git push origin dev
    ```

#### 5. Cleanup and Security Restoration
1.  Verify sync: `git status`.
2.  Mark tasks as complete.
