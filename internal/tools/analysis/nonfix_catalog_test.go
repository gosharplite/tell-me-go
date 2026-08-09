// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// sampleCatalog is a representative slice of the real
// docs/architect/INTENTIONAL_NON_FIXES.md format: ACCEPTED and non-ACCEPTED
// entries, single-line and range refs, indented continuation lines, ignored
// refs, and the *Last Updated* footer.
const sampleCatalog = `# Intentional Non-Fixes

Items evaluated by the architect and deliberately NOT pursued.

---

## Coverage Gaps (ACCEPTED)

### ado/pipeline_crud.go — buildVariablesUpdatePayload error return

- **Status**: ACCEPTED (2026-08)
- **Rationale**: The error return of json.Marshal is structurally unreachable.
- **See**: ` + "`internal/tools/integrations/ado/pipeline_crud.go:272-275`" + `
  (inline comment), and the branch at ` + "`pipeline_crud.go:308-310`" + ` that
  consumes this unreachable error

### persistence/mock_fs.go — Chmod always returns nil

- **Status**: accepted (2026-07) — lowercase status is matched case-insensitively
- **Rationale**: Mock no-op stub.
- **See**: ` + "`internal/domain/persistence/mock_fs.go:146-148`" + `

### history/history.go — complementPred at 0% → RESOLVED

- **Status**: RESOLVED (2026-07)
- **See**: ` + "`internal/infrastructure/history/history.go:301`" + `

### ui/tui/progress/model.go — handleDomainEvent (CC=12)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Type-switch dispatch.
- **See**: ` + "`internal/ui/tui/progress/model.go`" + `

### agent/agenttest + clitest — mock stubs at 0%

- **Status**: ACCEPTED (2026-08)
- **Rationale**: Interface-satisfying stubs.
- **See**: ` + "`internal/agent/agenttest/mock_cost_tracker.go:44`" + `,
  ` + "`internal/agent/agenttest/mock_logger.go:20`" + `,
  ` + "`internal/tools/workspace/shell.go:162,431,558`" + `

### agent/orchestrator/engine_phases.go — Process (CC=9)

- **Status**: [SUPERSEDED — see CC=12 entry below] ACCEPTED (2026-07)
- **See**: ` + "`internal/agent/orchestrator/engine_phases.go:101`" + `

### domain/config/config.go — validateProviderUniqueness

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Structural invariant.
- **See**: ` + "`internal/domain/config/config.go:183-185`" + `,
  commit ` + "`0f882423`" + ` (not a path:line ref, ignored)

---

*Last Updated: 2026-08 (catalog hygiene)*
`

func TestParseNonFixCatalog(t *testing.T) {
	t.Parallel()
	entries := parseNonFixCatalog(sampleCatalog)

	if len(entries) != 5 {
		t.Fatalf("parseNonFixCatalog() = %d entries, want 5", len(entries))
	}

	// Entries must be ACCEPTED and appear in document order.
	wantTitles := []string{
		"ado/pipeline_crud.go — buildVariablesUpdatePayload error return",
		"persistence/mock_fs.go — Chmod always returns nil",
		"ui/tui/progress/model.go — handleDomainEvent (CC=12)",
		"agent/agenttest + clitest — mock stubs at 0%",
		"domain/config/config.go — validateProviderUniqueness",
	}
	for i, want := range wantTitles {
		if entries[i].Title != want {
			t.Errorf("entries[%d].Title = %q, want %q", i, entries[i].Title, want)
		}
	}
}

func TestParseNonFixCatalog_Refs(t *testing.T) {
	t.Parallel()
	entries := parseNonFixCatalog(sampleCatalog)

	tests := []struct {
		name  string
		entry int
		want  []fileRange
	}{
		{
			name:  "full path range + bare basename range on continuation lines",
			entry: 0,
			want: []fileRange{
				{File: "internal/tools/integrations/ado/pipeline_crud.go", Start: 272, End: 275},
				{File: "pipeline_crud.go", Start: 308, End: 310},
			},
		},
		{
			name:  "single range ref",
			entry: 1,
			want: []fileRange{
				{File: "internal/domain/persistence/mock_fs.go", Start: 146, End: 148},
			},
		},
		{
			name:  "plain path without line numbers is ignored",
			entry: 2,
			want:  nil,
		},
		{
			name:  "continuation lines with comma-separated line lists",
			entry: 3,
			want: []fileRange{
				{File: "internal/agent/agenttest/mock_cost_tracker.go", Start: 44, End: 44},
				{File: "internal/agent/agenttest/mock_logger.go", Start: 20, End: 20},
				{File: "internal/tools/workspace/shell.go", Start: 162, End: 162},
				{File: "internal/tools/workspace/shell.go", Start: 431, End: 431},
				{File: "internal/tools/workspace/shell.go", Start: 558, End: 558},
			},
		},
		{
			name:  "commit hash in See bullet is ignored",
			entry: 4,
			want: []fileRange{
				{File: "internal/domain/config/config.go", Start: 183, End: 185},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := entries[tt.entry].Refs
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("entries[%d].Refs = %+v, want %+v", tt.entry, got, tt.want)
			}
		})
	}
}

func TestParseNonFixCatalog_Statuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  string
		include bool
	}{
		{"accepted", "ACCEPTED (2026-07)", true},
		{"accepted lowercase", "accepted (2026-07)", true},
		{"accepted with leading brackets", "[SUPERSEDED — ...] ACCEPTED", false},
		{"resolved", "RESOLVED (2026-07)", false},
		{"rejected", "REJECTED by architect", false},
		{"superseded", "SUPERSEDED", false},
		{"done", "DONE", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			md := "### Some Entry\n\n- **Status**: " + tt.status + "\n- **See**: `file.go:10`\n"
			entries := parseNonFixCatalog(md)
			if tt.include && len(entries) != 1 {
				t.Errorf("expected entry to be included, got %d entries", len(entries))
			}
			if !tt.include && len(entries) != 0 {
				t.Errorf("expected entry to be skipped, got %d entries", len(entries))
			}
		})
	}
}

func TestParseNonFixCatalog_IgnoresFooterAndProse(t *testing.T) {
	t.Parallel()
	md := "some leading prose\n\n### Valid Entry\n\n- **Status**: ACCEPTED\n- **See**: `file.go:1-5`\n\n*Last Updated: 2026-08*\n"
	entries := parseNonFixCatalog(md)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Title != "Valid Entry" {
		t.Errorf("title = %q, want %q", entries[0].Title, "Valid Entry")
	}
	if !strings.Contains(entries[0].Refs[0].File, "file.go") {
		t.Errorf("refs = %+v, want file.go ref", entries[0].Refs)
	}
}

func TestParseNonFixCatalog_NoHeading(t *testing.T) {
	t.Parallel()
	md := "# Intentional Non-Fixes\n\nProse about nothing.\n\n*Last Updated: 2026-08*\n"
	entries := parseNonFixCatalog(md)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for document without ### headings, got %d", len(entries))
	}
}

func TestParseCatalogRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ref  string
		want []fileRange
	}{
		{
			name: "single line",
			ref:  "internal/domain/config/config.go:183",
			want: []fileRange{{File: "internal/domain/config/config.go", Start: 183, End: 183}},
		},
		{
			name: "range",
			ref:  "internal/domain/config/config.go:183-185",
			want: []fileRange{{File: "internal/domain/config/config.go", Start: 183, End: 185}},
		},
		{
			name: "comma separated lines",
			ref:  "internal/tools/workspace/shell.go:162,431,558",
			want: []fileRange{
				{File: "internal/tools/workspace/shell.go", Start: 162, End: 162},
				{File: "internal/tools/workspace/shell.go", Start: 431, End: 431},
				{File: "internal/tools/workspace/shell.go", Start: 558, End: 558},
			},
		},
		{
			name: "comma separated ranges",
			ref:  "internal/agent/session/session_manager.go:142-144,237-240",
			want: []fileRange{
				{File: "internal/agent/session/session_manager.go", Start: 142, End: 144},
				{File: "internal/agent/session/session_manager.go", Start: 237, End: 240},
			},
		},
		{
			name: "empty comma part skipped",
			ref:  "file.go:10,,20",
			want: []fileRange{
				{File: "file.go", Start: 10, End: 10},
				{File: "file.go", Start: 20, End: 20},
			},
		},
		{
			name: "plain path ignored",
			ref:  "internal/ui/tui/progress/model.go",
			want: nil,
		},
		{
			name: "commit hash ignored",
			ref:  "0f882423",
			want: nil,
		},
		{
			name: "prose ignored",
			ref:  "prepareCompactedEntries",
			want: nil,
		},
		{
			name: "invalid line number ignored",
			ref:  "file.go:abc",
			want: nil,
		},
		{
			name: "inverted range ignored",
			ref:  "file.go:10-5",
			want: nil,
		},
		{
			name: "empty location ignored",
			ref:  "file.go:",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseCatalogRef(tt.ref)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseCatalogRef(%q) = %+v, want %+v", tt.ref, got, tt.want)
			}
		})
	}
}

// continuationCatalog exercises the See-block file-inheritance rules: a bare
// `:line` continuation ref inherits the file of the most recent file-bearing
// ref in the same See block (within one bullet and across indented
// continuation lines); a bare `:line` ref with no preceding file-bearing ref
// is dropped; file-only refs are dropped and do not feed inheritance.
const continuationCatalog = `# Intentional Non-Fixes

## Coverage Gaps (ACCEPTED)

### domain/config/config.go — validateProviderUniqueness

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Structural invariant.
- **See**: ` + "`internal/domain/config/config.go:224-229`" + ` (definition), ` + "`:249-251`" + ` (call-site error branch, covered by this entry)

### domain/services/task_service.go — AppendTask delegation wrapper

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Delegation wrapper.
- **See**: ` + "`internal/domain/services/task_service.go:95-97`" + ` (old anchor),
  ` + "`:101-103`" + ` (drifted body)

### agent/session/context/manager.go — bare ref with no inherited file

- **Status**: ACCEPTED (2026-07)
- **Rationale**: No file-bearing ref precedes the bare ref.
- **See**: ` + "`:574-576`" + `

### tools/workspace/shell.go — comma list after inherited file

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Comma lists keep expanding under inheritance.
- **See**: ` + "`internal/tools/workspace/shell.go:162`" + `,
  ` + "`:431,558`" + ` (inherits the file)

### ui/tui/progress/model.go — file-only ref does not feed inheritance

- **Status**: ACCEPTED (2026-07)
- **Rationale**: File-only refs are dropped and do not become inherited files.
- **See**: ` + "`internal/ui/tui/progress/model.go`" + `
  (file only), ` + "`:338`" + ` (must still be dropped)

---

*Last Updated: 2026-08 (catalog hygiene)*
`

func TestParseNonFixCatalog_ColonContinuationRefs(t *testing.T) {
	t.Parallel()
	entries := parseNonFixCatalog(continuationCatalog)

	if len(entries) != 5 {
		t.Fatalf("parseNonFixCatalog() = %d entries, want 5", len(entries))
	}

	tests := []struct {
		name  string
		entry int
		want  []fileRange
	}{
		{
			name:  "bare range inherits file within one See bullet",
			entry: 0,
			want: []fileRange{
				{File: "internal/domain/config/config.go", Start: 224, End: 229},
				{File: "internal/domain/config/config.go", Start: 249, End: 251},
			},
		},
		{
			name:  "bare range inherits file across indented continuation lines",
			entry: 1,
			want: []fileRange{
				{File: "internal/domain/services/task_service.go", Start: 95, End: 97},
				{File: "internal/domain/services/task_service.go", Start: 101, End: 103},
			},
		},
		{
			name:  "bare ref with no preceding file-bearing ref is dropped",
			entry: 2,
			want:  nil,
		},
		{
			name:  "comma list continues to expand under inherited file",
			entry: 3,
			want: []fileRange{
				{File: "internal/tools/workspace/shell.go", Start: 162, End: 162},
				{File: "internal/tools/workspace/shell.go", Start: 431, End: 431},
				{File: "internal/tools/workspace/shell.go", Start: 558, End: 558},
			},
		},
		{
			name:  "file-only ref does not feed inheritance",
			entry: 4,
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := entries[tt.entry].Refs
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("entries[%d].Refs = %+v, want %+v", tt.entry, got, tt.want)
			}
		})
	}
}

func TestParseCatalogRefInherited(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		ref           string
		inheritedFile string
		want          []fileRange
	}{
		{
			name:          "bare range inherits file",
			ref:           ":249-251",
			inheritedFile: "internal/domain/config/config.go",
			want:          []fileRange{{File: "internal/domain/config/config.go", Start: 249, End: 251}},
		},
		{
			name:          "bare single line inherits file",
			ref:           ":431",
			inheritedFile: "internal/tools/workspace/shell.go",
			want:          []fileRange{{File: "internal/tools/workspace/shell.go", Start: 431, End: 431}},
		},
		{
			name:          "bare comma list inherits file",
			ref:           ":431,558",
			inheritedFile: "internal/tools/workspace/shell.go",
			want: []fileRange{
				{File: "internal/tools/workspace/shell.go", Start: 431, End: 431},
				{File: "internal/tools/workspace/shell.go", Start: 558, End: 558},
			},
		},
		{
			name:          "bare ref with no inherited file dropped",
			ref:           ":249-251",
			inheritedFile: "",
			want:          nil,
		},
		{
			name:          "full ref ignores inherited file",
			ref:           "internal/other.go:10-12",
			inheritedFile: "internal/domain/config/config.go",
			want:          []fileRange{{File: "internal/other.go", Start: 10, End: 12}},
		},
		{
			name:          "file-only ref still dropped with inherited file",
			ref:           "internal/ui/tui/progress/model.go",
			inheritedFile: "internal/domain/config/config.go",
			want:          nil,
		},
		{
			name:          "commit hash still ignored with inherited file",
			ref:           "0f882423",
			inheritedFile: "internal/domain/config/config.go",
			want:          nil,
		},
		{
			name:          "empty location still ignored",
			ref:           "file.go:",
			inheritedFile: "internal/domain/config/config.go",
			want:          nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseCatalogRefInherited(tt.ref, tt.inheritedFile)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseCatalogRefInherited(%q, %q) = %+v, want %+v", tt.ref, tt.inheritedFile, got, tt.want)
			}
		})
	}
}

func TestLoadNonFixCatalog_MissingFile(t *testing.T) {
	t.Parallel()
	entries, err := loadNonFixCatalog(filepath.Join(t.TempDir(), "does-not-exist.md"))
	if err != nil {
		t.Fatalf("loadNonFixCatalog() error = %v, want nil for missing file", err)
	}
	if entries != nil && len(entries) != 0 {
		t.Errorf("expected empty entries for missing file, got %d", len(entries))
	}
}

func TestLoadNonFixCatalog_File(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "INTENTIONAL_NON_FIXES.md")
	if err := os.WriteFile(path, []byte(sampleCatalog), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	entries, err := loadNonFixCatalog(path)
	if err != nil {
		t.Fatalf("loadNonFixCatalog() error = %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("loadNonFixCatalog() = %d entries, want 5", len(entries))
	}
}

func TestLoadNonFixCatalog_UnreadableFile(t *testing.T) {
	t.Parallel()
	// A directory is not a readable catalog file: os.ReadFile returns an
	// error that is not IsNotExist, which must propagate.
	dir := t.TempDir()
	entries, err := loadNonFixCatalog(dir)
	if err == nil {
		t.Fatal("expected error for unreadable path, got nil")
	}
	if entries != nil {
		t.Errorf("expected nil entries on error, got %+v", entries)
	}
}

func TestCatalogTitleFor(t *testing.T) {
	t.Parallel()
	entries := []nonFixEntry{
		{
			Title: "entry A",
			Refs: []fileRange{
				{File: "internal/a.go", Start: 10, End: 10},
			},
		},
		{
			Title: "entry B",
			Refs: []fileRange{
				{File: "internal/b.go", Start: 5, End: 15},
			},
		},
	}

	if got := catalogTitleFor(entries, "internal/a.go", 10); got != "entry A" {
		t.Errorf("catalogTitleFor() = %q, want %q", got, "entry A")
	}
	if got := catalogTitleFor(entries, "internal/b.go", 7); got != "entry B" {
		t.Errorf("catalogTitleFor() = %q, want %q", got, "entry B")
	}
	if got := catalogTitleFor(entries, "internal/c.go", 10); got != "" {
		t.Errorf("catalogTitleFor() = %q, want empty for unknown file", got)
	}
}

func TestCatalogTitleForRange_Overlap(t *testing.T) {
	t.Parallel()
	entries := []nonFixEntry{
		{
			Title: "range entry",
			Refs: []fileRange{
				{File: "internal/ado/pipeline_crud.go", Start: 272, End: 275},
				{File: "pipeline_crud.go", Start: 308, End: 310}, // bare basename follow-on
			},
		},
	}

	tests := []struct {
		name  string
		file  string
		start int
		end   int
		want  string
	}{
		{
			name:  "block contained in ref range",
			file:  "internal/ado/pipeline_crud.go",
			start: 273,
			end:   274,
			want:  "range entry",
		},
		{
			name:  "block equal to ref range",
			file:  "internal/ado/pipeline_crud.go",
			start: 272,
			end:   275,
			want:  "range entry",
		},
		{
			name:  "partial overlap at start",
			file:  "internal/ado/pipeline_crud.go",
			start: 270,
			end:   273,
			want:  "range entry",
		},
		{
			name:  "partial overlap at end",
			file:  "internal/ado/pipeline_crud.go",
			start: 274,
			end:   280,
			want:  "range entry",
		},
		{
			name:  "no overlap before range",
			file:  "internal/ado/pipeline_crud.go",
			start: 100,
			end:   200,
			want:  "",
		},
		{
			name:  "no overlap after range",
			file:  "internal/ado/pipeline_crud.go",
			start: 300,
			end:   305,
			want:  "",
		},
		{
			name:  "bare basename ref matches full block path",
			file:  "internal/tools/integrations/ado/pipeline_crud.go",
			start: 308,
			end:   310,
			want:  "range entry",
		},
		{
			name:  "different file no match",
			file:  "internal/other.go",
			start: 308,
			end:   310,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := catalogTitleForRange(entries, tt.file, tt.start, tt.end)
			if got != tt.want {
				t.Errorf("catalogTitleForRange(%s, %d-%d) = %q, want %q", tt.file, tt.start, tt.end, got, tt.want)
			}
		})
	}
}

func TestCatalogTitleForRange_EmptyEntries(t *testing.T) {
	t.Parallel()
	if got := catalogTitleForRange(nil, "internal/a.go", 1, 10); got != "" {
		t.Errorf("expected empty title for nil entries, got %q", got)
	}
}

func TestNormalizeCatalogFilePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		path     string
		repoRoot string
		want     string
	}{
		{
			name:     "relative path already repo-relative",
			path:     "internal/agent/agent.go",
			repoRoot: "",
			want:     "internal/agent/agent.go",
		},
		{
			name:     "leading dot slash stripped",
			path:     "./internal/agent/agent.go",
			repoRoot: "",
			want:     "internal/agent/agent.go",
		},
		{
			name:     "absolute path under repo root",
			path:     "/work/tell-me-go/internal/agent/agent.go",
			repoRoot: "/work/tell-me-go",
			want:     "internal/agent/agent.go",
		},
		{
			name:     "relative path with dot repo root",
			path:     "internal/agent/agent.go",
			repoRoot: ".",
			want:     "internal/agent/agent.go",
		},
		{
			name:     "absolute path outside repo root stays as-is",
			path:     "/tmp/other/internal/agent/agent.go",
			repoRoot: "/work/tell-me-go",
			want:     "/tmp/other/internal/agent/agent.go",
		},
		{
			name:     "empty path",
			path:     "",
			repoRoot: "",
			want:     ".",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeCatalogFilePath(tt.path, tt.repoRoot)
			if got != tt.want {
				t.Errorf("normalizeCatalogFilePath(%q, %q) = %q, want %q", tt.path, tt.repoRoot, got, tt.want)
			}
		})
	}
}
