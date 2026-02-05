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
1.  **Check Cleanliness**: Run `git status`. Ensure the working directory is clean.
2.  **Enable Automation**: Execute `bypass_confirmation`.
3.  **Initialize Milestone**: Use `manage_tasks` to add: `"Public Release v1.x.x Readiness"`.
4.  **Sync Workspace**: Ensure you are on the `dev` branch and synchronized with remote: `git fetch origin && git checkout dev && git pull origin dev`.
5.  **Confirm Versioning**: Use `ask_user` to confirm the target release version (e.g., `1.81.0`) and the subsequent development version (e.g., `1.82.0-dev`). Store these in the `manage_scratchpad` for reference.

#### 2. Automated Readiness Verification
Run the following tool to perform a comprehensive security, dependency, and functional audit:
```bash
verify_release_readiness
```
**CRITICAL**: All checks in the generated report MUST return **[OK]**. If any check returns **[FAIL]**, you MUST fix the issue before proceeding.

#### 3. Version Stabilization
1.  Update `Version` in `cmd/tell-me-go/main.go` (remove `-dev` suffix, set to the confirmed release version).
2.  Run `go mod tidy`.
3.  **Note**: This project relies on Git history and tags for version tracking. A manual `CHANGELOG.md` is NOT maintained to ensure the Git log remains the single source of truth.
4.  Commit: `git commit -am "Chore: Stabilize version for release v1.x.x"` (use confirmed version).
5.  **Push to Remote**: `git push origin dev`.

#### 4. Git Tagging and Remote Synchronization
1.  **Sync and Merge into main**:
    ```bash
    git checkout main
    git fetch origin
    git reset --hard origin/main  # Safety: Ensure main matches remote truth and wipe unpushed commits
    git merge dev --no-ff         # Explicitly create a merge commit for the release record
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
    # Increment Version in cmd/tell-me-go/main.go to the confirmed next cycle version (e.g., "1.82.0-dev")
    git commit -am "Chore: Start next development cycle"
    git push origin dev
    ```

#### 5. Cleanup and Security Restoration
1.  **Verify Sync**: Run `git status` to ensure all branches are clean and synced.
2.  **Restore Security**: Execute `revoke_bypass` to re-enable interactive prompts.
3.  **Finalize Task**: Use `manage_tasks` (action: update) to mark the release task as `completed`.
