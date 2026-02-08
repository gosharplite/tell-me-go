package analysis

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

func TestInfoManager_RenderProjectSummary(t *testing.T) {
	m := &InfoManager{}

	modInfo := "module example.com/test\ngo 1.21\n"
	fileCounts := map[string]int{
		".go": 10,
		".md": 2,
	}
	packages := map[string]bool{
		"pkg1": true,
		"pkg2": true,
	}
	totalLOC := 1000

	got := m.renderProjectSummary(modInfo, fileCounts, packages, totalLOC)

	expectedContains := []string{
		"Project Summary:",
		"module example.com/test",
		"go 1.21",
		".go: 10",
		".md: 2",
		"Go Packages (2):",
		"- pkg1",
		"- pkg2",
		"Estimated Go LOC: 1000",
	}

	for _, want := range expectedContains {
		if !strings.Contains(got, want) {
			t.Errorf("renderProjectSummary() output does not contain %q\ngot: %s", want, got)
		}
	}
}

func TestInfoManager_ExtractGenericSkeleton(t *testing.T) {
	fs := storage.NewMockFileSystem()
	m := &InfoManager{FS: fs}
	ctx := context.Background()

	tests := []struct {
		name     string
		path     string
		content  string
		contains []string
	}{
		{
			name: "python like def",
			path: "test.py",
			content: `
# This is a comment
def my_func():
    print("hello")
`,
			contains: []string{
				"# This is a comment",
				"def my_func():",
			},
		},
		{
			name: "javascript like function",
			path: "test.js",
			content: `
/** doc */
function test() {
  return true;
}
`,
			contains: []string{
				"function test() {",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = fs.WriteFile(ctx, tt.path, []byte(tt.content), 0644)
			res, err := m.extractGenericSkeleton(ctx, tt.path)
			if err != nil {
				t.Fatalf("extractGenericSkeleton failed: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(res.Text, want) {
					t.Errorf("extractGenericSkeleton() output does not contain %q\ngot: %s", want, res.Text)
				}
			}
		})
	}
}

func TestInfoManager_ResolveModuleInfo(t *testing.T) {
	fs := storage.NewMockFileSystem()
	m := &InfoManager{FS: fs}
	ctx := context.Background()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "standard go.mod",
			content: "module github.com/test/repo\n\ngo 1.25\n\nrequire (\n\tgithub.com/pkg/errors v0.9.1\n)\n",
			want:    "module github.com/test/repo\ngo 1.25\n",
		},
		{
			name: "empty file",
		},
		{
			name:    "no module line",
			content: "go 1.25\n",
			want:    "go 1.25\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = fs.WriteFile(ctx, "go.mod", []byte(tt.content), 0644)
			got := m.resolveModuleInfo(ctx)
			if got != tt.want {
				t.Errorf("resolveModuleInfo() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInfoManager_CollectFileStats(t *testing.T) {
	fs := storage.NewMockFileSystem()
	m := &InfoManager{FS: fs}
	ctx := context.Background()

	// Setup mock files
	_ = fs.WriteFile(ctx, "main.go", []byte("package main\n\nfunc main() {}\n"), 0644)
	_ = fs.WriteFile(ctx, "internal/pkg/tools.go", []byte("package pkg\n\nfunc Tool() {}\n"), 0644)
	_ = fs.WriteFile(ctx, "README.md", []byte("# README\n"), 0644)
	_ = fs.WriteFile(ctx, ".git/config", []byte("git data"), 0644) // Should be skipped

	fileCounts, packages, totalLOC, err := m.collectFileStats(ctx)
	if err != nil {
		t.Fatalf("collectFileStats failed: %v", err)
	}

	if fileCounts[".go"] != 2 {
		t.Errorf("expected 2 .go files, got %d", fileCounts[".go"])
	}
	if fileCounts[".md"] != 1 {
		t.Errorf("expected 1 .md file, got %d", fileCounts[".md"])
	}
	if fileCounts[".git"] != 0 {
		t.Errorf("expected 0 .git files (directory should be skipped), got %d", fileCounts[".git"])
	}

	if len(packages) != 2 {
		t.Errorf("expected 2 packages, got %d", len(packages))
	}
	if !packages["."] || !packages["internal/pkg"] {
		t.Errorf("packages map missing expected entries: %+v", packages)
	}

	// 3 lines in main.go, 3 lines in internal/pkg/tools.go
	if totalLOC != 6 {
		t.Errorf("expected 6 total LOC, got %d", totalLOC)
	}
}

func TestInfoManager_GetProjectSummary(t *testing.T) {
	fs := storage.NewMockFileSystem()
	m := &InfoManager{FS: fs}
	ctx := context.Background()

	_ = fs.WriteFile(ctx, "go.mod", []byte("module example.com/test\ngo 1.25\n"), 0644)
	_ = fs.WriteFile(ctx, "main.go", []byte("package main\n"), 0644)

	res, err := m.GetProjectSummary(ctx, nil)
	if err != nil {
		t.Fatalf("GetProjectSummary failed: %v", err)
	}

	if !strings.Contains(res.Text, "Project Summary:") {
		t.Errorf("GetProjectSummary() output missing 'Project Summary:', got: %s", res.Text)
	}
	if !strings.Contains(res.Text, "example.com/test") {
		t.Errorf("GetProjectSummary() output missing module name, got: %s", res.Text)
	}
}

func TestInfoManager_CollectFileStats_EdgeCases(t *testing.T) {
	fs := storage.NewMockFileSystem()
	m := &InfoManager{FS: fs}
	ctx := context.Background()

	t.Run("empty directory", func(t *testing.T) {
		fileCounts, packages, totalLOC, err := m.collectFileStats(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(fileCounts) != 0 || len(packages) != 0 || totalLOC != 0 {
			t.Errorf("expected empty results, got counts=%v, pkgs=%v, loc=%d", fileCounts, packages, totalLOC)
		}
	})

	t.Run("files without extensions", func(t *testing.T) {
		_ = fs.WriteFile(ctx, "LICENSE", []byte("MIT"), 0644)
		_ = fs.WriteFile(ctx, "Makefile", []byte("all: test"), 0644)

		fileCounts, _, _, _ := m.collectFileStats(ctx)
		if fileCounts["(no ext)"] != 2 {
			t.Errorf("expected 2 files with (no ext), got %d", fileCounts["(no ext)"])
		}
	})

	t.Run("skip directories", func(t *testing.T) {
		_ = fs.WriteFile(ctx, "vendor/pkg/v.go", []byte("package v"), 0644)
		_ = fs.WriteFile(ctx, "node_modules/lib/n.js", []byte("const x = 1"), 0644)

		fileCounts, packages, _, _ := m.collectFileStats(ctx)
		if fileCounts[".go"] != 0 {
			t.Errorf("expected 0 .go files (vendor skipped), got %d", fileCounts[".go"])
		}
		if len(packages) != 0 {
			t.Errorf("expected 0 packages, got %d", len(packages))
		}
	})
}
