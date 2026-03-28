<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# Standard Operating Procedure (SOP): Public Release Process

### Objective
This SOP defines the automated workflow for publishing a new public release of the `tell-me-go` project.

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
    git fetch origin && git checkout dev && git pull origin dev
    ```
3.  **Confirm Target Version**: Use `git tag -l` to check the last release version. Use `ask_user` to present the last version and propose the target release version (e.g., `v1.1.0`).

#### 2. Automated Readiness Verification
Run the following tool to perform a comprehensive security, dependency, and functional audit:
```bash
verify_release_readiness
```
**CRITICAL**: All checks in the generated report MUST return **[OK]**. If any check returns **[FAIL]**, you MUST fix the issue before proceeding.

#### 3. Preparation
1.  **Note**: This project relies on Git tags as the single source of truth for versioning. The version is injected at build time using Go linker flags (`ldflags`).
2.  Run `go mod tidy` to ensure `go.sum` is up-to-date.
3.  **Final Build Check**: Run `make build VERSION=vX.Y.Z` (using your target version) to ensure everything compiles with the version flag.

#### 4. Git Tagging and Remote Synchronization
1.  **Sync and Merge into main**:
    ```bash
    git checkout main
    git fetch origin
    git reset --hard origin/main  # Safety: Ensure main matches remote truth
    git merge dev --no-ff -m "Release version vX.Y.Z"
    ```
2.  **Tag the release**:
    ```bash
    git tag -a vX.Y.Z -m "Release version vX.Y.Z"
    ```
3.  **Build and Verify Binary**:
    ```bash
    make build VERSION=vX.Y.Z
    ```
    Verify the binary reports the correct version: `./tell-me-go --version` (or relevant version command).
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

---

### Code Templates

#### Version Injection (ldflags)
If the `Makefile` is unavailable, use this template:
```bash
go build -ldflags="-X 'main.version=vX.Y.Z'" -o tell-me-go ./cmd/tell-me-go
```

---

### Verification/Testing
1.  **Tag Existence**: `git tag -l vX.Y.Z` must return the new tag.
2.  **Binary Integrity**: `./tell-me-go --version` must output exactly `vX.Y.Z`.
3.  **Remote State**: Check the remote repository (e.g., GitHub/Azure DevOps) to ensure tags and branch updates are visible.
4.  **Readiness Audit**: The `verify_release_readiness` report must be reviewed.

---

### Best Practices
- **Never Re-tag**: If a release fails after tagging, increment the patch version (e.g., `v1.1.0` -> `v1.1.1`) instead of moving the tag.
- **Fast-Forward Forbidden**: Always use `--no-ff` when merging to `main` to preserve release history.
- **Atomic Release**: Do not commit any other changes to `main` during the release process except the merge from `dev`.
- **Pre-tag Sync**: Always run `git fetch origin` before tagging to avoid conflicts with tags created by others.

---

### Implementation Checklist
- [x] Target version `vX.Y.Z` is confirmed by user.
- [x] `verify_release_readiness` returned all **[OK]**.
- [x] `go mod tidy` executed.
- [x] `dev` merged into `main` with `--no-ff`.
- [x] Git tag `vX.Y.Z` created and verified locally.
- [x] Binary built with correct `ldflags` and version verified.
- [x] Changes and tags pushed to remote origin.
