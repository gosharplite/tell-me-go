<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# Standard Operating Procedure (SOP): Binary Release Publication

### Objective
This SOP defines the process for publishing cross-compiled platform binaries to GitHub Releases. It is a **supplementary, optional step** that runs after the main [Public Release Process](public_release.md). The main release handles branch synchronization, tagging, and verification. This SOP adds downloadable binaries for users who prefer not to install the Go toolchain.

---

### Prerequisites
- Go toolchain 1.26+.
- Git CLI (configured with appropriate remote access).
- **GitHub CLI (`gh`)** installed and authenticated.
- Active session with `bypass_confirmation` enabled.
- The main **Public Release Process** ([public_release.md](public_release.md)) is fully complete:
  - Tag `vX.Y.Z` exists locally and on `origin`.
  - `origin/main` and `origin/dev` converge to the same commit.
  - Post-flight convergence check passes.

---

### Step-by-Step Instructions

#### 1. Pre-flight Verification
1. **Check gh CLI**: Run `gh auth status`. If the command fails, stop and tell the user to install and authenticate the GitHub CLI (`gh auth login`).
2. **Verify main release convergence**:
   ```bash
   git fetch origin
   git rev-list --count origin/main..origin/dev
   git rev-list --count origin/dev..origin/main
   ```
   **Both MUST output `0`.** If not, the main release is incomplete. Stop and tell the user to complete [public_release.md](public_release.md) first.

#### 2. Identify and Confirm Target Version
1. **Find the latest tag**:
   ```bash
   git describe --tags --abbrev=0
   ```
2. **Confirm with the user**: Present the detected tag and ask: *"Publish binaries for `<tag>`?"* Do not proceed without explicit user approval.
3. Store the confirmed tag as `$TAG` for subsequent steps.

#### 3. Run the Binary Release Script
Execute:
```bash
./scripts/release-binaries.sh $TAG
```

The script performs:
1. Validates the tag exists locally and on `origin`.
2. Cross-compiles for 6 platform/architecture pairs:

   | OS | Architectures |
   |---|---|
   | Linux | amd64, arm64 |
   | macOS | amd64, arm64 |
   | Windows | amd64, arm64 |

3. Packages each binary (`.tar.gz` for Unix, `.zip` for Windows).
4. Generates `tell-me-go_checksums.txt` with SHA-256 hashes.
5. Creates the GitHub Release (or appends artifacts if the release already exists).

**CRITICAL**: If the script fails at any step, stop and diagnose. Do not retry without understanding the failure.

#### 4. Verify the Release
1. Open the release page for visual inspection:
   ```bash
   gh release view $TAG --web
   ```
2. Confirm all 7 files are attached (6 archives + 1 checksum file).
3. Smoke-test the current-platform binary:
   ```bash
   # Determine current OS and arch
   OS=$(uname -s | tr '[:upper:]' '[:lower:]')
   ARCH=$(uname -m)
   # Map to our naming convention
   case "$ARCH" in
     x86_64)  ARCH="amd64" ;;
     aarch64) ARCH="arm64" ;;
   esac

   # Download and verify
   curl -L "https://github.com/gosharplite/tell-me-go/releases/download/$TAG/tell-me-go-${OS}-${ARCH}.tar.gz" | tar xz
   ./tell-me-go-${OS}-${ARCH} --version
   ```
   The version output MUST match `$TAG`.

#### 5. Cleanup
1. Remove the downloaded test binary:
   ```bash
   rm -f tell-me-go-*
   ```
2. Remove the local `dist/` directory:
   ```bash
   rm -rf dist/
   ```

---

### Platforms Published

| File | OS | Arch |
|---|---|---|
| `tell-me-go-linux-amd64.tar.gz` | Linux | x86_64 |
| `tell-me-go-linux-arm64.tar.gz` | Linux | ARM64 |
| `tell-me-go-darwin-amd64.tar.gz` | macOS | Intel |
| `tell-me-go-darwin-arm64.tar.gz` | macOS | Apple Silicon |
| `tell-me-go-windows-amd64.zip` | Windows | x86_64 |
| `tell-me-go-windows-arm64.zip` | Windows | ARM64 |
| `tell-me-go_checksums.txt` | — | SHA-256 hashes |

---

### User Installation from Binaries

After publication, users can install without Go:

**Linux/macOS (amd64):**
```bash
curl -L https://github.com/gosharplite/tell-me-go/releases/download/$TAG/tell-me-go-linux-amd64.tar.gz | tar xz
sudo mv tell-me-go-linux-amd64 /usr/local/bin/tell-me-go
```

**macOS (Apple Silicon):**
```bash
curl -L https://github.com/gosharplite/tell-me-go/releases/download/$TAG/tell-me-go-darwin-arm64.tar.gz | tar xz
sudo mv tell-me-go-darwin-arm64 /usr/local/bin/tell-me-go
```

**Windows (PowerShell):**
```powershell
Invoke-WebRequest -Uri https://github.com/gosharplite/tell-me-go/releases/download/$TAG/tell-me-go-windows-amd64.zip -OutFile tell-me-go.zip
Expand-Archive tell-me-go.zip -DestinationPath .
```

---

### Verification/Testing
1. **Artifact count**: 7 files attached to the GitHub Release (6 binaries + checksums).
2. **Checksum validity**: `sha256sum -c tell-me-go_checksums.txt` in the download directory.
3. **Version output**: Downloaded binary reports the correct version with `--version`.
4. **Cross-platform smoke**: At minimum, test the current-OS binary before declaring success.

---

### Best Practices
- **Run after main release only**: The tag and branch convergence must be complete before publishing binaries. This SOP does not create or move tags.
- **Never re-upload to a different tag**: If a binary is broken, delete the release and re-tag with an incremented patch version.
- **Idempotent**: Re-running the script on the same tag uploads artifacts with `--clobber`, replacing any existing files. It will not create duplicate releases.
- **No Makefile dependency**: This script is entirely standalone. It uses `go build` directly with the same `ldflags` pattern as the Makefile's `build` target.

---

### Relationship to Other SOPs
| SOP | When |
|---|---|
| [public_release.md](public_release.md) | **First** — creates tag, merges branches, verifies readiness. |
| **binary_release.md** (this SOP) | **Second** — publishes downloadable binaries to the existing tag. |
| [self_update_safety.md](self_update_safety.md) | Whenever modifying core agent entry points. |


### Implementation Checklist
- [x] `gh auth status` passes.
- [x] Branch convergence verified (`0` commits ahead/behind both ways).
- [x] Latest tag identified and user confirmed.
- [x] `./scripts/release-binaries.sh $TAG` completed successfully.
- [x] GitHub Release page verified (7 files).
- [x] Current-platform binary smoke-tested, version matches.
- [x] `dist/` and test binary cleaned up.
