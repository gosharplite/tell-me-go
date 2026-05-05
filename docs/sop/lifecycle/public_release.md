<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# Standard Operating Procedure (SOP): Public Release Process

### Objective
This SOP defines the automated workflow for publishing a new public release of the `tell-me-go` project. The branch synchronization policy enforced by this SOP is documented in ADR-030 (Release Branch Synchronization Policy).

---

### Prerequisites
- Go toolchain 1.26+.
- Git CLI (configured with appropriate remote access).
- Clean working directory (verified by `git status`).
- Active session with `bypass_confirmation` enabled.

---

### Step-by-Step Instructions

#### 1. Task Initialization
1.  **Check Cleanliness**: Run `git status`. Ensure the working directory is clean.
2.  **Sync Workspace**: Ensure you are on the `dev` branch and synchronized with remote:
    ```bash
    git fetch origin
    git checkout dev
    git pull --ff-only origin dev
    ```
3.  **Pre-flight Divergence Check (ABORT GATE)**: Verify that `dev` is strictly ahead of `main`. This catches two failure modes before any release work begins:
    - **Empty release**: `dev` has no new commits to publish.
    - **Back-merge gap**: `main` has commits that `dev` does not (a previous release was not back-merged per Step 4.6).
    ```bash
    git fetch origin
    AHEAD=$(git rev-list --count origin/main..origin/dev)
    BEHIND=$(git rev-list --count origin/dev..origin/main)
    echo "dev is $AHEAD commit(s) ahead of main, $BEHIND commit(s) behind."
    ```
    **Required state**: `AHEAD > 0` AND `BEHIND == 0`.
    *   If `AHEAD == 0`: there is nothing to release. **Abort the SOP.**
    *   If `BEHIND > 0`: a prior release was not back-merged. **Abort the SOP** and recover by running:
        ```bash
        git checkout dev
        git merge origin/main --no-ff -m "chore: sync missed release(s) back to dev"
        git push origin dev
        ```
        Then restart this SOP from Step 1.
4.  **Confirm Target Version**: Use `git tag -l` to check the last release version. Use `ask_user` to present the last version and propose the target release version (e.g., `v1.1.0`).

#### 2. Automated Readiness Verification
Run the following tool to perform a comprehensive security, dependency, and functional audit:
```bash
verify_release_readiness
```
**CRITICAL**: All checks in the generated report MUST return **[OK]**. If any check returns **[FAIL]**, you MUST fix the issue before proceeding.

#### 3. Preparation
1.  **Note**: This project relies on Git tags as the single source of truth for versioning. The version is injected at build time using Go linker flags (`ldflags`).
2.  **Cleanup**: Ensure any existing `tell-me-go` or `tell-me-go.exe` binaries are removed.
    *   **Tooling**: Use the `delete_path` tool.
    *   **Manual Fallback**: If the tool is blocked, use `del tell-me-go.exe` (Windows) or `rm -f tell-me-go` (Linux/macOS).
3.  Run `go mod tidy` to ensure `go.sum` is up-to-date.
4. **Final Build Check**: Run the build command for your current OS to ensure everything compiles correctly:
    *   **Windows**:
        ```bash
        go build -ldflags -X=main.version=vX.Y.Z -o tell-me-go.exe ./cmd/tell-me-go
        ```
    *   **Linux/macOS**:
        ```bash
        go build -ldflags -X=main.version=vX.Y.Z -o tell-me-go ./cmd/tell-me-go
        ```

#### 4. Git Tagging and Remote Synchronization
1.  **Sync and Merge into main**:
    ```bash
    git checkout main
    git fetch origin
    git pull --ff-only origin main
    git merge dev --no-ff -m "Release version vX.Y.Z"
    ```
2.  **Build and Verify Binary on the merge commit (BEFORE tagging)**:
    Tags are immutable. Tag only what you have verified compiles and runs.
    ```bash
    go build -ldflags -X=main.version=vX.Y.Z -o tell-me-go ./cmd/tell-me-go
    ```
    Verify the binary reports the correct version.
    *   **On Windows**: `tell-me-go --version` (or `tell-me-go.exe --version`).
    *   **On Linux/macOS**: `./tell-me-go --version`.
3.  **Smoke test the merge commit**:
    Run the race-enabled test suite against the actual merge commit to catch any semantic conflicts that a clean text merge could hide.
    ```bash
    go test -race -count=1 ./...
    ```
    All packages MUST pass. If any test fails, abort the release: run `git reset --hard origin/main` to discard the local merge commit, fix the issue on `dev`, and restart from Step 1.
4.  **Tag the verified release**:
    ```bash
    git tag -a vX.Y.Z -m "Release version vX.Y.Z"
    ```
5.  **Push main and tag first**:
    ```bash
    git push origin main --tags
    ```
6.  **Back-merge main into dev (CRITICAL — do not skip)**:
    This step ensures `dev` and `main` converge to the same commit after every release. Skipping it causes permanent branch divergence.
    ```bash
    git checkout dev
    git merge main --ff-only
    ```
    If `--ff-only` fails because new commits were pushed to `dev` during the release, resolve by:
    ```bash
    git pull --rebase origin dev
    git merge main --no-ff -m "chore: sync release vX.Y.Z back to dev"
    ```
7.  **Push dev**:
    ```bash
    git push origin dev
    ```
8.  **Post-flight convergence check**:
    ```bash
    git fetch origin
    git rev-list --count origin/main..origin/dev   # MUST output 0
    git rev-list --count origin/dev..origin/main   # MUST output 0
    ```
    If either command outputs a non-zero number, the release is **incomplete**. Investigate before declaring success.

#### 5. Cleanup and Security Restoration
1.  **Verify Sync**: Run `git status` to ensure all branches are clean and synced.
2.  **Binary Cleanup**: Use `delete_path` to remove the release binary created for verification.

---

### Code Templates

#### Version Injection (ldflags)
Use this command to build the binary with a specific version string (environment-agnostic syntax):
```bash
go build -ldflags -X=main.version=vX.Y.Z -o tell-me-go ./cmd/tell-me-go
```

---

### Verification/Testing
1.  **Tag Existence**: `git tag -l vX.Y.Z` must return the new tag.
2.  **Binary Integrity**: Running the binary with `--version` must output exactly `vX.Y.Z`.
3.  **Remote State**: Check the remote repository (e.g., GitHub/Azure DevOps) to ensure tags and branch updates are visible.
4.  **Readiness Audit**: The `verify_release_readiness` report must be reviewed.

---

### Best Practices
- **Environment-Agnostic Syntax**: Use `-X=key=value` for `ldflags` to avoid platform-specific quoting issues.
- **Never Re-tag**: If a release fails after tagging, increment the patch version (e.g., `v1.1.0` -> `v1.1.1`) instead of moving the tag.
- **Fast-Forward Forbidden**: Always use `--no-ff` when merging to `main` to preserve release history.
- **Atomic Release**: Do not commit any other changes to `main` during the release process except the merge from `dev`.
- **Pre-tag Sync**: Always run `git fetch origin` before tagging to avoid conflicts with tags created by others.
- **Atomic Operations**: Run commands individually instead of chaining with `&&` to simplify debugging and recovery.
- **No Destructive Resets**: Use `git pull --ff-only` instead of `git reset --hard` when synchronizing local branches. The former surfaces unexpected divergence; the latter silently discards it.
- **Always Back-Merge**: Every release MUST end with `origin/main` and `origin/dev` pointing to the same commit. The release is not complete until both `git rev-list --count origin/main..origin/dev` and `git rev-list --count origin/dev..origin/main` return `0`.

---

### Implementation Checklist
- [x] Target version `vX.Y.Z` is confirmed by user.
- [x] `verify_release_readiness` returned all **[OK]**.
- [x] `go mod tidy` executed.
- [x] `dev` merged into `main` with `--no-ff`.
- [x] Git tag `vX.Y.Z` created and verified locally.
- [x] Binary built with correct `ldflags` and version verified.
- [x] `main` and tag pushed to remote origin.
- [x] `main` back-merged into `dev` (convergence restored).
- [x] `dev` pushed to remote origin.
- [x] Post-flight check: `origin/main` and `origin/dev` are at the same commit.
