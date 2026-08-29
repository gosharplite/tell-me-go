package analysis

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
)

// buildTagExcluded reports whether the .go file at dir/base is excluded from
// the host build by build constraints (//go:build line or filename suffix,
// e.g. proc_windows.go on non-Windows hosts). R2, issue #1455: such files are
// never compiled by `go tool` on this host, so the complexity walk must skip
// them and report them as skipped instead of analyzing them.
// label is the build-tag description for the skip report: the //go:build
// expression when present, else the filename-suffix tags, else "unspecified".
// On a MatchFile error the file is treated as included (analyze normally;
// a genuinely broken file soft-fails in the parse path).
func buildTagExcluded(dir, base string) (label string, excluded bool) {
	included, err := build.Default.MatchFile(dir, base)
	if err != nil || included {
		return "", false
	}
	if l := headerBuildLabel(filepath.Join(dir, base)); l != "" {
		return l, true
	}
	if l := suffixLabel(base); l != "" {
		return l, true
	}
	return "unspecified", true
}

// headerBuildLabel scans the file header for a //go:build or // +build line,
// stopping at the first non-blank, non-comment line. Returns "" when the
// header carries no build constraint or the file cannot be read.
func headerBuildLabel(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			if strings.HasPrefix(trimmed, "//go:build ") {
				return strings.TrimSpace(strings.TrimPrefix(trimmed, "//go:build "))
			}
			if strings.HasPrefix(trimmed, "// +build ") {
				return strings.TrimSpace(strings.TrimPrefix(trimmed, "// +build "))
			}
			continue
		}
		break // first non-blank, non-comment line: the header has ended
	}
	return ""
}

// suffixLabel derives the build-tag label from the filename suffix:
// proc_windows.go → "windows", shell_windows_test.go → "windows",
// x_windows_encoding_test.go → "windows/encoding". Returns "" when the base
// carries no suffix tag.
func suffixLabel(base string) string {
	s := strings.TrimSuffix(base, ".go")
	s = strings.TrimSuffix(s, "_test")
	parts := strings.Split(s, "_")
	if len(parts) > 1 {
		return strings.Join(parts[1:], "/")
	}
	return ""
}
