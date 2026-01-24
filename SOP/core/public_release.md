# Standard Operating Procedure (SOP): Public Release Process

### Objective
This SOP defines the requirements and steps for publishing a new public release of the `tell-me-go` project, ensuring code quality, security compliance, and comprehensive documentation for a Go-based application.

---

### Prerequisites
- All tests must pass: `go test ./...`.
- `go mod tidy` has been run and `go.sum` is up to date.
- The `README.md` must be updated with the latest features and usage examples.
- A clean Git state (no uncommitted or untracked experimental files).
- Access to the repository with permission to push tags.

---

### Step-by-Step Instructions

#### 1. Security & Privacy Audit
Before any public release, perform a mandatory security scan:
- **Secret Scanning**: Ensure no Service Account JSON keys (`*.json`), API keys, or `.env` files are in the repository. Use `git grep` for common patterns like `"private_key"` or `"api_key"`.
- **Ignore Check**: Verify `.gitignore` covers `output/`, `*.log`, and any local session files.
- **Privacy**: Ensure no personal data or proprietary internal URLs are hardcoded in the Go source code or YAML configs.

#### 2. Documentation & Compliance Review
- **License**: Verify `LICENSE` (MIT) is present in the root.
- **SPDX Headers**: Ensure all Go source files (`*.go`), YAML files, and Markdown files (`*.md`) in the `SOP/` directory contain the standard SPDX-License-Identifier header.
    - **⚠️ CRITICAL**: Modifications to core entry points (in `cmd/`) **MUST** follow the safety procedures in `SOP/core/self_update_safety.md`.
- **SOP Sync**: Verify that the `SOP/` directory reflects the current project architecture.
- **Version Bump**: Update any version constants in the source code (e.g., in a `version` package or the main CLI help text).

#### 3. Final Functional Verification
Run the full suite in a clean environment:
```bash
go mod tidy
go fmt ./...
go vet ./...
go test -race ./...
go build ./...
```
*Note: The Git pre-commit requirements defined in `SOP/core/git_workflow.md` provide the final safety gate before merging to `main`.*

#### 4. Changelog Update
Create or update a `CHANGELOG.md` or the "Latest Changes" section of the README:
- Categorize changes into: `Added`, `Changed`, `Fixed`, `Removed`.
- Highlight Go-specific improvements (e.g., "Optimized concurrency with worker pools").

#### 5. Git Tagging and Pushing
Follow Semantic Versioning (vMAJOR.MINOR.PATCH):
1.  **Switch to Main Branch**: `git checkout main`.
2.  **Merge Dev**: `git merge dev`.
3.  **Tag the release**:
    ```bash
    git tag -a v1.0.0 -m "Release version 1.0.0 - [Brief summary]"
    ```
4.  **Push**:
    ```bash
    git push origin main --tags
    ```

---

### Release Checklist
- [ ] Security audit completed (no secrets found).
- [ ] `go test ./...` returns **PASS**.
- [ ] `go mod tidy` run and `go.sum` committed.
- [ ] `README.md` includes all new features and configuration options.
- [ ] `SOP/` directory is updated to reflect structural changes.
- [ ] Branch `dev` is successfully merged into `main`.
- [ ] Git tag is applied and pushed.

---

### Best Practices
- **Release Often**: Small, frequent releases are easier to audit and test.
- **Binary Verification**: If distributing binaries, verify they run on target architectures (Linux/macOS/Windows).
- **Draft Releases**: Use GitHub's "Draft Release" feature to stage the release notes.
- **Minimal Dependencies**: Maintain the project's philosophy by preferring the Go standard library and avoiding unnecessary external packages.

