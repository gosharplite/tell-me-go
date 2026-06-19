package analysis

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

func TestInfoManager_RenderProjectSummary(t *testing.T) {
	m := &infoManager{Policy: infra_persistence.NewWorkspacePolicy()}

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
	fs := persistence.NewMockFileSystem()
	m := &infoManager{FS: fs, Policy: infra_persistence.NewWorkspacePolicy()}
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
	fs := persistence.NewMockFileSystem()
	m := &infoManager{FS: fs, Policy: infra_persistence.NewWorkspacePolicy()}
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
	fs := persistence.NewMockFileSystem()
	m := &infoManager{FS: fs, Policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	// Setup mock files
	_ = fs.WriteFile(ctx, "main.go", []byte("package main\n\nfunc main() {}\n"), 0644)
	_ = fs.WriteFile(ctx, "internal/pkg/tools.go", []byte("package pkg\n\nfunc Tool() {}\n"), 0644)
	_ = fs.WriteFile(ctx, "README.md", []byte("# README\n"), 0644)
	_ = fs.WriteFile(ctx, ".git/config", []byte("git data"), 0644) // Should be skipped

	fileCounts, packages, totalLOC, err := m.collectFileStats(ctx, nil)
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
	fs := persistence.NewMockFileSystem()
	m := &infoManager{FS: fs, Policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	_ = fs.WriteFile(ctx, "go.mod", []byte("module example.com/test\ngo 1.25\n"), 0644)
	_ = fs.WriteFile(ctx, "main.go", []byte("package main\n"), 0644)

	res, err := m.GetProjectSummary(ctx, nil, nil)
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
	fs := persistence.NewMockFileSystem()
	m := &infoManager{FS: fs, Policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	t.Run("empty directory", func(t *testing.T) {
		fileCounts, packages, totalLOC, err := m.collectFileStats(ctx, nil)
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

		fileCounts, _, _, _ := m.collectFileStats(ctx, nil)
		if fileCounts["(no ext)"] != 2 {
			t.Errorf("expected 2 files with (no ext), got %d", fileCounts["(no ext)"])
		}
	})

	t.Run("skip directories", func(t *testing.T) {
		_ = fs.WriteFile(ctx, "vendor/pkg/v.go", []byte("package v"), 0644)
		_ = fs.WriteFile(ctx, "node_modules/lib/n.js", []byte("const x = 1"), 0644)

		fileCounts, packages, _, _ := m.collectFileStats(ctx, nil)
		if fileCounts[".go"] != 0 {
			t.Errorf("expected 0 .go files (vendor skipped), got %d", fileCounts[".go"])
		}
		if len(packages) != 0 {
			t.Errorf("expected 0 packages, got %d", len(packages))
		}
	})
}

func TestInfoManager_GoDoc(t *testing.T) {
	runner := &mockAnalysisGoRunner{
		getGoDocFunc: func(ctx context.Context, symbol string) ([]byte, error) {
			if symbol != "fmt.Println" {
				return nil, fmt.Errorf("unexpected symbol: %s", symbol)
			}
			return []byte("GoDoc output"), nil
		},
	}
	m := &infoManager{Runner: runner, Policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	args := map[string]interface{}{
		"symbol": "fmt.Println",
	}

	res, err := m.GoDoc(ctx, args, nil)
	if err != nil {
		t.Fatalf("GoDoc failed: %v", err)
	}

	if res.Text != "GoDoc output" {
		t.Errorf("expected 'GoDoc output', got %q", res.Text)
	}
}

func TestInfoManager_SendHeartbeat(t *testing.T) {
	t.Parallel()

	t.Run("nil channel does not panic", func(t *testing.T) {
		t.Parallel()
		m := &infoManager{}
		m.sendHeartbeat(nil, 50)
	})

	t.Run("not divisible by 50", func(t *testing.T) {
		t.Parallel()
		m := &infoManager{}
		hb := make(chan struct{}, 1)
		m.sendHeartbeat(hb, 49)
		select {
		case <-hb:
			t.Error("expected no heartbeat for count=49")
		default:
		}
	})

	t.Run("divisible by 50 sends heartbeat", func(t *testing.T) {
		t.Parallel()
		m := &infoManager{}
		hb := make(chan struct{}, 1)
		m.sendHeartbeat(hb, 50)
		select {
		case <-hb:
		default:
			t.Error("expected heartbeat for count=50")
		}
	})
}

func TestInfoManager_GetFileSkeleton(t *testing.T) {
	t.Parallel()

	t.Run("go file skeleton", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		m := &infoManager{
			SP:     &mockSecurityProvider{},
			FS:     persistence.NewMockFileSystem(),
			Cache:  newASTCache(tmpDir),
			Policy: infra_persistence.NewWorkspacePolicy(),
		}
		ctx := context.Background()

		srcPath := tmpDir + "/test.go"
		if err := os.WriteFile(srcPath, []byte("package test\n\ntype Exported struct {\n\tField int\n}\n"), 0644); err != nil {
			t.Fatal(err)
		}

		res, err := m.GetFileSkeleton(ctx, map[string]interface{}{"filepath": srcPath}, nil)
		if err != nil {
			t.Fatalf("GetFileSkeleton failed: %v", err)
		}
		if !strings.Contains(res.Text, "type Exported struct") {
			t.Errorf("expected 'type Exported struct' in skeleton, got: %s", res.Text)
		}
		if !strings.Contains(res.Text, "SYSTEM HINT") {
			t.Error("expected SYSTEM HINT in output")
		}
	})

	t.Run("non-go file generic skeleton", func(t *testing.T) {
		t.Parallel()
		fs := persistence.NewMockFileSystem()
		m := &infoManager{
			SP:     &mockSecurityProvider{},
			FS:     fs,
			Policy: infra_persistence.NewWorkspacePolicy(),
		}
		ctx := context.Background()
		_ = fs.WriteFile(ctx, "/test.py", []byte("def my_func(): pass"), 0644)

		res, err := m.GetFileSkeleton(ctx, map[string]interface{}{"filepath": "/test.py"}, nil)
		if err != nil {
			t.Fatalf("GetFileSkeleton failed: %v", err)
		}
		if !strings.Contains(res.Text, "def my_func():") {
			t.Errorf("expected 'def my_func():' in output, got: %s", res.Text)
		}
	})

	t.Run("go file parse failure falls back to generic", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		fs := persistence.NewMockFileSystem()
		m := &infoManager{
			SP:     &mockSecurityProvider{},
			FS:     fs,
			Cache:  newASTCache(tmpDir),
			Policy: infra_persistence.NewWorkspacePolicy(),
		}
		ctx := context.Background()

		invalidPath := tmpDir + "/broken.go"
		invalidContent := "not valid go"
		// Write to real disk for AST cache (will fail parse), and to mock FS for fallback
		if err := os.WriteFile(invalidPath, []byte(invalidContent), 0644); err != nil {
			t.Fatal(err)
		}
		_ = fs.WriteFile(ctx, invalidPath, []byte(invalidContent), 0644)

		res, err := m.GetFileSkeleton(ctx, map[string]interface{}{"filepath": invalidPath}, nil)
		if err != nil {
			t.Fatalf("GetFileSkeleton failed: %v", err)
		}
		// Should fall back to generic skeleton extraction
		if !strings.Contains(res.Text, "SYSTEM HINT") {
			t.Error("expected SYSTEM HINT in output even for unparseable go file")
		}
	})
}

// errorFileSystem implements persistence.FileSystem and always returns error from Walk, ReadFile, and Open.
// Used to test error paths in GetProjectSummary, collectFileStats, and countLines.
type errorFileSystem struct {
	persistence.FileSystem
}

func (e *errorFileSystem) Walk(ctx context.Context, root string, fn persistence.WalkFunc) error {
	return fmt.Errorf("walk error")
}

func (e *errorFileSystem) ReadFile(ctx context.Context, name string) ([]byte, error) {
	return nil, fmt.Errorf("read error")
}

func (e *errorFileSystem) Open(ctx context.Context, name string) (persistence.File, error) {
	return nil, fmt.Errorf("open error")
}

// fakeFileInfo implements os.FileInfo for use in tests that need a minimal FileInfo value.
type fakeFileInfo struct{}

func (f fakeFileInfo) Name() string       { return "test.go" }
func (f fakeFileInfo) Size() int64        { return 100 }
func (f fakeFileInfo) Mode() os.FileMode  { return 0644 }
func (f fakeFileInfo) ModTime() time.Time { return time.Now() }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() interface{}   { return nil }

func TestGetFileSkeleton_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("UnmarshalArgs error", func(t *testing.T) {
		t.Parallel()
		m := &infoManager{SP: &mockSecurityProvider{}, Policy: infra_persistence.NewWorkspacePolicy()}
		_, err := m.GetFileSkeleton(context.Background(), map[string]interface{}{"filepath": make(chan int)}, nil)
		if err == nil {
			t.Error("expected error for unserializable args")
		}
	})

	t.Run("IsPathSafe error", func(t *testing.T) {
		t.Parallel()
		m := &infoManager{SP: &denyingSecurityProvider{}, Policy: infra_persistence.NewWorkspacePolicy()}
		_, err := m.GetFileSkeleton(context.Background(), map[string]interface{}{"filepath": "/some/path"}, nil)
		if err == nil {
			t.Error("expected error from IsPathSafe rejection")
		}
	})

	t.Run("non-go file with FS read error", func(t *testing.T) {
		t.Parallel()
		fs := persistence.NewMockFileSystem()
		m := &infoManager{
			SP:     &mockSecurityProvider{},
			FS:     fs,
			Policy: infra_persistence.NewWorkspacePolicy(),
		}
		// Don't write the file — FS.ReadFile will fail
		_, err := m.GetFileSkeleton(context.Background(), map[string]interface{}{"filepath": "/nonexistent.py"}, nil)
		if err == nil {
			t.Error("expected error from FS.ReadFile failure for non-go file")
		}
	})

	t.Run("go file fallback with FS read error", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		fs := persistence.NewMockFileSystem()
		m := &infoManager{
			SP:     &mockSecurityProvider{},
			FS:     fs,
			Cache:  newASTCache(tmpDir),
			Policy: infra_persistence.NewWorkspacePolicy(),
		}
		// Path is .go; AST cache fails (file not on real disk), fallback to extractGenericSkeleton
		// which also fails because the mock FS has no such file
		_, err := m.GetFileSkeleton(context.Background(), map[string]interface{}{"filepath": tmpDir + "/nonexistent.go"}, nil)
		if err == nil {
			t.Error("expected error from FS.ReadFile failure in go fallback path")
		}
	})
}

func TestGetProjectSummary_ErrorPath(t *testing.T) {
	t.Parallel()
	// Use a mock FS that always returns an error from Walk
	fs := &errorFileSystem{}
	m := &infoManager{FS: fs, Policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	_, err := m.GetProjectSummary(ctx, nil, nil)
	if err == nil {
		t.Error("expected error from FS.Walk failure")
	}
}

func TestPublishGoDocEvent_NilEvents(t *testing.T) {
	t.Parallel()
	m := &infoManager{Events: nil, Policy: infra_persistence.NewWorkspacePolicy()}
	// Must not panic
	m.publishGoDocEvent(context.Background(), "fmt.Println")
}

func TestFormatDocOutput_Error(t *testing.T) {
	t.Parallel()
	m := &infoManager{Policy: infra_persistence.NewWorkspacePolicy()}

	t.Run("error with partial output", func(t *testing.T) {
		result := m.formatDocOutput([]byte("partial output"), fmt.Errorf("go doc failed"))
		if !strings.Contains(result.Text, "Error running go doc") {
			t.Errorf("expected error message in output, got: %s", result.Text)
		}
		if !strings.Contains(result.Text, "partial output") {
			t.Errorf("expected partial output in result, got: %s", result.Text)
		}
	})

	t.Run("error with empty output omits Output label", func(t *testing.T) {
		result := m.formatDocOutput(nil, fmt.Errorf("go doc failed"))
		if !strings.Contains(result.Text, "Error running go doc") {
			t.Errorf("expected error message in output, got: %s", result.Text)
		}
		if strings.Contains(result.Text, "Output:") {
			t.Errorf("expected no 'Output:' label when output is empty, got: %s", result.Text)
		}
	})
}

// TestInfoManager_RemainingErrorPaths covers error branches not exercised by other tests.
//
// Scenarios covered:
//
//	A. recordGoStats: countLines returns an error → silently ignored (totalLOC unchanged)
//	B. makeWalkFunc: Walk passes non-nil err to WalkFunc → returns nil (silent skip)
//	C. countLines: scanner.Err() after partial scan → defensive branch; untriggerable in unit
//	   tests without a mock file that fails mid-read; covered by scanner contract.
//	D. countLines: FS.Open returns an error → error propagated
func TestInfoManager_RemainingErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("recordGoStats_countLines_error", func(t *testing.T) {
		t.Parallel()
		// Scenario A: cache miss + FS.Open error in countLines
		fs := &errorFileSystem{}
		m := &infoManager{
			FS:     fs,
			Policy: infra_persistence.NewWorkspacePolicy(),
			Cache:  nil, // force cache miss
		}
		stats := &projectStats{
			fileCounts: make(map[string]int),
			packages:   make(map[string]bool),
		}
		m.recordGoStats(context.Background(), "test.go", fakeFileInfo{}, stats)
		// Should not panic; totalLOC stays 0 (error silently ignored)
		if stats.totalLOC != 0 {
			t.Errorf("expected totalLOC=0 after countLines error, got %d", stats.totalLOC)
		}
	})

	t.Run("makeWalkFunc_walk_error_param", func(t *testing.T) {
		t.Parallel()
		// Scenario B: Walk passes error to WalkFunc
		m := &infoManager{Policy: infra_persistence.NewWorkspacePolicy()}
		stats := &projectStats{fileCounts: make(map[string]int), packages: make(map[string]bool)}
		wf := m.makeWalkFunc(context.Background(), stats, nil)
		err := wf("/some/path", fakeFileInfo{}, fmt.Errorf("permission denied"))
		// Should return nil (silent skip)
		if err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("countLines_open_error", func(t *testing.T) {
		t.Parallel()
		// Scenario D: FS.Open returns error
		fs := &errorFileSystem{}
		m := &infoManager{FS: fs, Policy: infra_persistence.NewWorkspacePolicy()}
		loc, err := m.countLines(context.Background(), "test.go")
		if err == nil {
			t.Error("expected error from FS.Open, got nil")
		}
		if loc != 0 {
			t.Errorf("expected loc=0 on error, got %d", loc)
		}
	})
}

// mockEventBus is a minimal EventBus for testing publishGoDocEvent.
type mockEventBus struct {
	publishFunc func(ctx context.Context, e events.Event) error
}

func (m *mockEventBus) Publish(ctx context.Context, e events.Event) error {
	if m.publishFunc != nil {
		return m.publishFunc(ctx, e)
	}
	return nil
}
func (m *mockEventBus) Subscribe(sub func(context.Context, events.Event)) {}
func (m *mockEventBus) Shutdown(ctx context.Context) error                { return nil }
func (m *mockEventBus) Flush(ctx context.Context) error                   { return nil }
func (m *mockEventBus) Listen(ctx context.Context) error                  { return nil }
func (m *mockEventBus) WaitStarted()                                      {}

func TestInfoManager_IsTargetSourceFile(t *testing.T) {
	t.Parallel()
	m := &infoManager{Policy: infra_persistence.NewWorkspacePolicy()}

	tests := []struct {
		name     string
		fileName string
		isDir    bool
		want     bool
	}{
		{"directory returns false", "foo.go", true, false},
		{"go file returns true", "main.go", false, true},
		{"test file returns false", "main_test.go", false, false},
		{"non-go file returns false", "README.md", false, false},
		{"no extension returns false", "Makefile", false, false},
		{"go file in subdir", "pkg/handler.go", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.isTargetSourceFile(tt.fileName, tt.isDir)
			if got != tt.want {
				t.Errorf("isTargetSourceFile(%q, %v) = %v, want %v", tt.fileName, tt.isDir, got, tt.want)
			}
		})
	}
}

func TestInfoManager_RecordGoStats_CacheHit(t *testing.T) {
	t.Parallel()

	// Create a real temp file that can be cached by astCache
	tmpDir := t.TempDir()
	srcPath := tmpDir + "/test.go"
	content := "package test\n\nfunc main() {}\n"
	if err := os.WriteFile(srcPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cache := newASTCache(tmpDir)
	// Populate the cache by calling Get (this stores modTime and lineCount)
	_, _, err := cache.Get("test.go")
	if err != nil {
		t.Fatalf("cache.Get failed: %v", err)
	}

	info, err := os.Stat(srcPath)
	if err != nil {
		t.Fatalf("os.Stat failed: %v", err)
	}

	fs := persistence.NewMockFileSystem()
	m := &infoManager{
		FS:     fs,
		Cache:  cache,
		Policy: infra_persistence.NewWorkspacePolicy(),
	}

	stats := &projectStats{
		fileCounts: make(map[string]int),
		packages:   make(map[string]bool),
	}

	// recordGoStats with a populated cache — should hit the cache
	m.recordGoStats(context.Background(), "test.go", info, stats)

	// Verify cache hit: totalLOC should be 3 (3 lines in the file)
	if stats.totalLOC != 3 {
		t.Errorf("expected totalLOC=3 from cache hit, got %d", stats.totalLOC)
	}
	if !stats.packages["."] {
		t.Error("expected '.' package to be registered")
	}
}

func TestInfoManager_GoDoc_Error(t *testing.T) {
	t.Parallel()

	runner := &mockAnalysisGoRunner{
		getGoDocFunc: func(ctx context.Context, symbol string) ([]byte, error) {
			return []byte("partial output"), fmt.Errorf("go doc failure")
		},
	}
	m := &infoManager{Runner: runner, Policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	args := map[string]interface{}{"symbol": "nonexistent.Symbol"}
	res, err := m.GoDoc(ctx, args, nil)
	if err != nil {
		t.Fatalf("GoDoc should return nil error (error is embedded in result): %v", err)
	}

	if !strings.Contains(res.Text, "Error running go doc: go doc failure") {
		t.Errorf("expected error message in result.Text, got: %s", res.Text)
	}
	if !strings.Contains(res.Text, "Output: partial output") {
		t.Errorf("expected partial output in result.Text, got: %s", res.Text)
	}
}

func TestInfoManager_PublishGoDocEvent_WithEvents(t *testing.T) {
	t.Parallel()

	published := make(chan string, 1)
	bus := &mockEventBus{
		publishFunc: func(ctx context.Context, e events.Event) error {
			select {
			case published <- e.(events.SystemMessageEvent).Message:
			default:
			}
			return nil
		},
	}

	m := &infoManager{Events: bus, Policy: infra_persistence.NewWorkspacePolicy()}
	m.publishGoDocEvent(context.Background(), "fmt.Println")

	select {
	case msg := <-published:
		if !strings.Contains(msg, "Running go doc fmt.Println") {
			t.Errorf("unexpected message: %s", msg)
		}
	case <-time.After(time.Second):
		t.Error("expected event to be published")
	}
}

func TestInfoManager_StartGoDocHeartbeat(t *testing.T) {
	t.Parallel()

	t.Run("nil hb does not panic", func(t *testing.T) {
		t.Parallel()
		m := &infoManager{Policy: infra_persistence.NewWorkspacePolicy()}
		done := m.startGoDocHeartbeat(nil)
		// Closing done stops the goroutine
		close(done)
		// Give goroutine time to exit
		time.Sleep(10 * time.Millisecond)
	})

	t.Run("non-nil hb receives heartbeat", func(t *testing.T) {
		t.Parallel()
		m := &infoManager{Policy: infra_persistence.NewWorkspacePolicy()}
		hb := make(chan struct{}, 1)
		done := m.startGoDocHeartbeat(hb)

		// Wait for ticker to fire (2s interval)
		select {
		case <-hb:
			// Received heartbeat
		case <-time.After(3 * time.Second):
			t.Error("expected heartbeat within 3s")
		}
		close(done) // stop the goroutine
	})
}

func TestInfoManager_GoDoc_HappyPath_WithHeartbeat(t *testing.T) {
	t.Parallel()

	runner := &mockAnalysisGoRunner{
		getGoDocFunc: func(ctx context.Context, symbol string) ([]byte, error) {
			return []byte("doc output"), nil
		},
	}
	m := &infoManager{Runner: runner, Policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	hb := make(chan struct{}, 2)
	res, err := m.GoDoc(ctx, map[string]interface{}{"symbol": "fmt.Println"}, hb)
	if err != nil {
		t.Fatalf("GoDoc failed: %v", err)
	}
	if res.Text != "doc output" {
		t.Errorf("expected 'doc output', got %q", res.Text)
	}
}

func TestInfoManager_GoDoc_UnmarshalArgsError(t *testing.T) {
	t.Parallel()
	m := &infoManager{Policy: infra_persistence.NewWorkspacePolicy()}
	// Pass unserializable value to trigger UnmarshalArgs error
	_, err := m.GoDoc(context.Background(), map[string]interface{}{"symbol": make(chan int)}, nil)
	if err == nil {
		t.Error("expected error from UnmarshalArgs failure")
	}
}

func TestInfoManager_PublishGoDocEvent_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("SafePublish returns ErrBusNotInitialized (silently ignored)", func(t *testing.T) {
		t.Parallel()
		bus := &mockEventBus{
			publishFunc: func(ctx context.Context, e events.Event) error {
				return events.ErrBusNotInitialized
			},
		}
		m := &infoManager{Events: bus, Policy: infra_persistence.NewWorkspacePolicy()}
		// Must not panic; error is silently suppressed
		m.publishGoDocEvent(context.Background(), "fmt.Println")
	})

	t.Run("SafePublish returns other error (logged)", func(t *testing.T) {
		t.Parallel()
		bus := &mockEventBus{
			publishFunc: func(ctx context.Context, e events.Event) error {
				return fmt.Errorf("some publish error")
			},
		}
		m := &infoManager{Events: bus, Policy: infra_persistence.NewWorkspacePolicy()}
		// Must not panic; error is logged via slog.Default()
		m.publishGoDocEvent(context.Background(), "fmt.Println")
	})
}
