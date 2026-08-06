// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// fileRange identifies a single file:line or file:start-end reference in the
// Intentional Non-Fixes catalog.
type fileRange struct {
	File  string
	Start int
	End   int
}

// nonFixEntry is a single ACCEPTED entry from the Intentional Non-Fixes
// catalog: a known gap deliberately left unfixed, with the file references
// that pin it to source locations.
type nonFixEntry struct {
	Title string
	Refs  []fileRange
}

// defaultNonFixCatalogPath is the location of the Intentional Non-Fixes
// catalog relative to the current working directory. The catalog is
// architect-curated; the analysis tooling only reads it.
var defaultNonFixCatalogPath = filepath.Join("docs", "architect", "INTENTIONAL_NON_FIXES.md")

// backtickRef matches a backtick-delimited reference (`path:line`) inside a
// See bullet or its indented continuation lines.
var backtickRef = regexp.MustCompile("`([^`]+)`")

// pendingEntry accumulates one catalog entry while its Status bullet is still
// being scanned; only entries whose status is ACCEPTED are kept.
type pendingEntry struct {
	entry  nonFixEntry
	status string
}

// loadNonFixCatalog reads and parses the Intentional Non-Fixes catalog at
// path. Entries under `### ` headings whose Status bullet matches ACCEPTED
// (case-insensitive prefix) are returned with their `- **See**:` references
// parsed into fileRange values. RESOLVED, REJECTED, SUPERSEDED and DONE
// entries are skipped. The *Last Updated* footer and anything outside a
// `### ` heading are ignored.
//
// A missing catalog degrades gracefully: it returns an empty slice with a nil
// error so callers can treat "no catalog" as "no accepted gaps".
func loadNonFixCatalog(path string) ([]nonFixEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseNonFixCatalog(string(data)), nil
}

// parseNonFixCatalog parses catalog markdown into ACCEPTED entries.
func parseNonFixCatalog(data string) []nonFixEntry {
	var pending []pendingEntry
	var cur *pendingEntry
	inSee := false

	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "### ") {
			pending = append(pending, pendingEntry{
				entry: nonFixEntry{Title: strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))},
			})
			cur = &pending[len(pending)-1]
			inSee = false
			continue
		}

		if cur == nil {
			continue // footer or prose outside a ### heading
		}

		switch {
		case strings.HasPrefix(trimmed, "- **Status**:"):
			cur.status = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "- **Status**:")))
			inSee = false
		case strings.HasPrefix(trimmed, "- **See**:"):
			inSee = true
			parseSeeRefs(trimmed, cur)
		case strings.HasPrefix(trimmed, "- "):
			inSee = false
		case inSee && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")):
			// Indented continuation of a See bullet carrying more refs.
			parseSeeRefs(trimmed, cur)
		}
	}

	var accepted []nonFixEntry
	for _, p := range pending {
		if strings.HasPrefix(p.status, "accepted") {
			accepted = append(accepted, p.entry)
		}
	}
	return accepted
}

// parseSeeRefs extracts every backticked reference from a See bullet line (or
// an indented continuation) and appends the valid fileRange values to the
// pending entry.
func parseSeeRefs(line string, cur *pendingEntry) {
	for _, m := range backtickRef.FindAllStringSubmatch(line, -1) {
		cur.entry.Refs = append(cur.entry.Refs, parseCatalogRef(m[1])...)
	}
}

// parseCatalogRef parses one backticked `path:line` or `path:start-end`
// reference into one or more fileRange values. A comma-separated location
// list (`file.go:162,431,558` or `file.go:142-144,237-240`) expands into
// multiple ranges. Refs that are not `<path>:<line>` shaped (commit hashes,
// plain paths, prose) are ignored.
func parseCatalogRef(ref string) []fileRange {
	colonIdx := strings.LastIndex(ref, ":")
	if colonIdx == -1 {
		return nil
	}
	file := strings.TrimSpace(ref[:colonIdx])
	locSpec := strings.TrimSpace(ref[colonIdx+1:])
	if file == "" || locSpec == "" {
		return nil
	}

	var ranges []fileRange
	for _, part := range strings.Split(locSpec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if dashIdx := strings.Index(part, "-"); dashIdx != -1 {
			start, err1 := strconv.Atoi(part[:dashIdx])
			end, err2 := strconv.Atoi(part[dashIdx+1:])
			if err1 != nil || err2 != nil || start < 1 || end < start {
				continue
			}
			ranges = append(ranges, fileRange{File: file, Start: start, End: end})
			continue
		}
		line, err := strconv.Atoi(part)
		if err != nil || line < 1 {
			continue
		}
		ranges = append(ranges, fileRange{File: file, Start: line, End: line})
	}
	return ranges
}

// catalogTitleFor returns the Title of the first ACCEPTED catalog entry whose
// references contain the given line, or "" if none does.
func catalogTitleFor(entries []nonFixEntry, file string, line int) string {
	return catalogTitleForRange(entries, file, line, line)
}

// catalogTitleForRange returns the Title of the first ACCEPTED catalog entry
// whose references overlap the block range [start,end]. A block overlaps a
// reference when block.Start <= ref.End && block.End >= ref.Start. Returns ""
// when no entry matches.
func catalogTitleForRange(entries []nonFixEntry, file string, start, end int) string {
	for _, e := range entries {
		for _, ref := range e.Refs {
			if start > ref.End || end < ref.Start {
				continue
			}
			if refMatchesFile(ref.File, file) {
				return e.Title
			}
		}
	}
	return ""
}

// refMatchesFile reports whether a catalog reference file matches a block
// file. Full paths must match exactly; a bare basename (no path separator) in
// the catalog matches any block file with that basename — catalog entries
// occasionally pin a follow-on range with a bare name (e.g.
// "pipeline_crud.go:308-310") after a full-path reference.
func refMatchesFile(refFile, blockFile string) bool {
	if refFile == blockFile {
		return true
	}
	if strings.Contains(refFile, "/") || strings.Contains(refFile, "\\") {
		return false
	}
	return filepath.Base(blockFile) == refFile
}

// normalizeCatalogFilePath converts a funcComplexity.FilePath produced by
// fs.Walk (absolute, or relative to the walk root such as ".") into the
// repo-relative form used by the catalog ("internal/..."). When the repo root
// is unknown or normalization fails, the cleaned path is returned unchanged
// so callers can treat the function as uncataloged.
func normalizeCatalogFilePath(path, repoRoot string) string {
	p := filepath.Clean(path)
	if repoRoot != "" {
		if rel, err := filepath.Rel(repoRoot, p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			p = rel
		}
	}
	p = strings.TrimPrefix(p, "./")
	return filepath.ToSlash(p)
}
