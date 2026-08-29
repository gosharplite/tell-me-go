package analysis

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBuildTagExcluded is the R2 (issue #1455) table-driven unit contract for
// buildTagExcluded. Real files on disk are required — go/build.MatchFile
// reads from the host filesystem. Host-relative by definition.
func TestBuildTagExcluded(t *testing.T) {
	t.Parallel()

	writeFile := func(t *testing.T, dir, base, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, base), []byte(content), 0644))
	}

	t.Run("go:build constraint excludes", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "tagged.go", "//go:build anunexpectedtag\n\npackage x\nfunc W() {}\n")

		label, excluded := buildTagExcluded(dir, "tagged.go")
		require.True(t, excluded, "anunexpectedtag matches no host build context")
		require.Equal(t, "anunexpectedtag", label)
	})

	t.Run("filename suffix excludes", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("filename suffix compiles on windows; guarantee is host-relative")
		}
		dir := t.TempDir()
		writeFile(t, dir, "x_windows.go", "package x\nfunc W() {}\n")

		label, excluded := buildTagExcluded(dir, "x_windows.go")
		require.True(t, excluded)
		require.Equal(t, "windows", label)
	})

	t.Run("unconstrained file included", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "plain.go", "package x\n")

		label, excluded := buildTagExcluded(dir, "plain.go")
		require.False(t, excluded)
		require.Empty(t, label)
	})

	t.Run("readable label from //go:build wins over suffix", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("build-tag file compiles on windows; guarantee is host-relative")
		}
		dir := t.TempDir()
		writeFile(t, dir, "x_windows.go", "//go:build windows\n\npackage x\nfunc W() {}\n")

		label, excluded := buildTagExcluded(dir, "x_windows.go")
		require.True(t, excluded)
		require.Equal(t, "windows", label)
	})
}
