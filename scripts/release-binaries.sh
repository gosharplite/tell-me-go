#!/usr/bin/env bash
# release-binaries — cross-compile and publish binaries to GitHub Releases.
#
# Usage:
#   ./scripts/release-binaries.sh v1.0.0
#
# Prerequisites:
#   - Go 1.26+
#   - gh CLI (authenticated)
#   - Working directory is the repo root
#   - The tag already exists and is pushed to origin
#
# Produces:
#   dist/
#     tell-me-go-linux-amd64.tar.gz
#     tell-me-go-linux-arm64.tar.gz
#     tell-me-go-darwin-amd64.tar.gz
#     tell-me-go-darwin-arm64.tar.gz
#     tell-me-go-windows-amd64.zip
#     tell-me-go-windows-arm64.zip
#     tell-me-go_checksums.txt
#
# All artifacts are uploaded to the GitHub Release for the given tag.
set -euo pipefail

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  echo "Usage: $0 <version-tag>" >&2
  echo "Example: $0 v1.0.0" >&2
  exit 1
fi

# ── 0. Pre-flight checks ───────────────────────────────────────────────────

# Verify the tag exists locally
if ! git tag -l "$VERSION" | grep -qFx "$VERSION"; then
  echo "ERROR: tag '$VERSION' not found locally. Create and push it first." >&2
  exit 1
fi

# Verify the tag exists on the remote
if ! git ls-remote --tags origin "$VERSION" | grep -q .; then
  echo "ERROR: tag '$VERSION' not found on origin. Push it first." >&2
  exit 1
fi

# Verify gh CLI is available
if ! command -v gh &>/dev/null; then
  echo "ERROR: gh CLI not found. Install it: https://cli.github.com/" >&2
  exit 1
fi

# Verify gh is authenticated
if ! gh auth status &>/dev/null; then
  echo "ERROR: gh not authenticated. Run: gh auth login" >&2
  exit 1
fi

# ── 1. Build ───────────────────────────────────────────────────────────────

echo "=== Building binaries for $VERSION ==="

PLATFORMS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
  "windows/arm64"
)

rm -rf dist
mkdir -p dist

for platform in "${PLATFORMS[@]}"; do
  GOOS="${platform%/*}"
  GOARCH="${platform#*/}"
  OUT="dist/tell-me-go-${GOOS}-${GOARCH}"
  if [ "$GOOS" = "windows" ]; then
    OUT="${OUT}.exe"
  fi

  echo "  Building $GOOS/$GOARCH → $OUT"

  GOOS="$GOOS" GOARCH="$GOARCH" \
    go build \
      -ldflags="-X 'main.version=${VERSION}'" \
      -o "$OUT" \
      ./cmd/tell-me-go
done

# ── 2. Package ─────────────────────────────────────────────────────────────

echo ""
echo "=== Packaging ==="

CHECKSUMS="dist/tell-me-go_checksums.txt"
> "$CHECKSUMS"  # Create empty file

for platform in "${PLATFORMS[@]}"; do
  GOOS="${platform%/*}"
  GOARCH="${platform#*/}"
  SRC="dist/tell-me-go-${GOOS}-${GOARCH}"

  if [ "$GOOS" = "windows" ]; then
    ARCHIVE="tell-me-go-${GOOS}-${GOARCH}.zip"
    echo "  Packaging $ARCHIVE"
    zip -j "dist/$ARCHIVE" "${SRC}.exe" >/dev/null
    rm "${SRC}.exe"
  else
    ARCHIVE="tell-me-go-${GOOS}-${GOARCH}.tar.gz"
    echo "  Packaging $ARCHIVE"
    tar czf "dist/$ARCHIVE" -C dist "tell-me-go-${GOOS}-${GOARCH}"
    rm "$SRC"
  fi

  # Append checksum
  (cd dist && sha256sum "$ARCHIVE") >> "$CHECKSUMS"
done

# ── 3. Publish GitHub Release ──────────────────────────────────────────────

echo ""
echo "=== Publishing GitHub Release ==="

# Check if release already exists
if gh release view "$VERSION" &>/dev/null; then
  echo "  Release $VERSION already exists. Uploading artifacts..."
  gh release upload "$VERSION" \
    dist/tell-me-go-*.tar.gz \
    dist/tell-me-go-*.zip \
    "$CHECKSUMS" \
    --clobber
else
  echo "  Creating release $VERSION..."
  gh release create "$VERSION" \
    dist/tell-me-go-*.tar.gz \
    dist/tell-me-go-*.zip \
    "$CHECKSUMS" \
    --title "$VERSION" \
    --notes "Binary release for $VERSION.

See [public_release.md](docs/sop/lifecycle/public_release.md) for the full release process."
fi

# ── 4. Summary ─────────────────────────────────────────────────────────────

echo ""
echo "=== Done ==="
echo "Release: https://github.com/gosharplite/tell-me-go/releases/tag/$VERSION"
echo ""
echo "Artifacts:"
ls -lh dist/*.tar.gz dist/*.zip dist/*.txt 2>/dev/null || true
echo ""
echo "Users can now install via:"
echo "  curl -L https://github.com/gosharplite/tell-me-go/releases/download/$VERSION/tell-me-go-linux-amd64.tar.gz | tar xz"
