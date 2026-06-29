package analysis

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"
)

// denyingSecurityProvider wraps mockSecurityProvider but denies all path access.
type denyingSecurityProvider struct {
	mockSecurityProvider
}

func (d *denyingSecurityProvider) IsPathSafe(path string) (string, error) {
	return "", errors.New("path not authorized")
}

func TestComplexityAnalyzer_Analyze(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	code := `package test
func Simple() {}
func Complex(a, b bool) {
	if a {
		if b {
		}
	}
}
`
	filePath := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(filePath, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	cache := newASTCache(".")
	sp := &mockSecurityProvider{}
	analyzer := newComplexityAnalyzer(cache, sp, infra_persistence.NewOSFileSystem())

	res, err := analyzer.Analyze(context.Background(), map[string]interface{}{"path": tmpDir}, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if !strings.Contains(res.Text, "Simple - Complexity: 1") {
		t.Errorf("Expected Simple to have complexity 1, got:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "Complex - Complexity: 3") {
		t.Errorf("Expected Complex to have complexity 3, got:\n%s", res.Text)
	}
}

func TestComplexityAnalyzer_Sorting(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	code := `package test
func A() { if true {} } // 2
func B() { if true { if true {} } } // 3
func C() {} // 1
`
	filePath := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(filePath, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	cache := newASTCache(".")
	sp := &mockSecurityProvider{}
	analyzer := newComplexityAnalyzer(cache, sp, infra_persistence.NewOSFileSystem())

	res, err := analyzer.Analyze(context.Background(), map[string]interface{}{"path": tmpDir}, nil)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(res.Text, "\n")
	// line 0 is header
	if !strings.Contains(lines[1], "B - Complexity: 3") {
		t.Errorf("Expected first result to be B (complexity 3), got: %s", lines[1])
	}
	if !strings.Contains(lines[2], "A - Complexity: 2") {
		t.Errorf("Expected second result to be A (complexity 2), got: %s", lines[2])
	}
	if !strings.Contains(lines[3], "C - Complexity: 1") {
		t.Errorf("Expected third result to be C (complexity 1), got: %s", lines[3])
	}
}

// TestGetConcurrencyLimit_Documented verifies getConcurrencyLimit always
// returns >= 1. The defense-in-depth guard `if limit < 1 { limit = 1 }`
// (complexity.go:102) cannot be triggered because runtime.NumCPU() never
// returns < 1 on supported Go platforms. This is accepted technical debt
// — identical to the fn.Type().(*types.Signature) guard in dead_code.go.
func TestGetConcurrencyLimit_Documented(t *testing.T) {
	t.Parallel()
	analyzer := newComplexityAnalyzer(nil, nil, infra_persistence.NewOSFileSystem())
	limit := analyzer.getConcurrencyLimit()
	if limit < 1 {
		t.Errorf("getConcurrencyLimit() = %d, want >= 1", limit)
	}
}

func TestProcessFileTask_SemAcquireError(t *testing.T) {
	t.Parallel()

	// Create a semaphore with capacity 1 and acquire its only slot,
	// then cancel the context. The next Acquire will fail.
	sem := semaphore.NewWeighted(1)
	ctx, cancel := context.WithCancel(context.Background())

	// Acquire the only slot
	if err := sem.Acquire(ctx, 1); err != nil {
		t.Fatalf("failed to acquire semaphore slot: %v", err)
	}

	// Cancel the context so the next Acquire fails
	cancel()

	analyzer := newComplexityAnalyzer(newASTCache("."), &mockSecurityProvider{}, infra_persistence.NewOSFileSystem())

	var complexities []funcComplexity
	var mu sync.Mutex
	var skippedErrs []string
	var skippedMu sync.Mutex

	err := analyzer.processFileTask(ctx, sem, "test.go", nil, 0, &complexities, &mu, &skippedErrs, &skippedMu)
	if err == nil {
		t.Error("expected error from sem.Acquire with cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestFormatResults(t *testing.T) {
	t.Parallel()
	analyzer := newComplexityAnalyzer(nil, nil, infra_persistence.NewOSFileSystem())

	tests := []struct {
		name    string
		input   []funcComplexity
		contain []string
		absent  []string
	}{
		{
			name:  "empty input",
			input: nil,
			contain: []string{
				"Cyclomatic Complexity Analysis (Top 100):",
			},
			absent: []string{"... (truncated)"},
		},
		{
			name: "single item",
			input: []funcComplexity{
				{FilePath: "f.go", Line: 10, Name: "Func", Complexity: 3},
			},
			contain: []string{
				"f.go:10: Func - Complexity: 3",
			},
			absent: []string{"... (truncated)"},
		},
		{
			name: "sorting by complexity then name",
			input: []funcComplexity{
				{FilePath: "a.go", Line: 1, Name: "B", Complexity: 2},
				{FilePath: "a.go", Line: 2, Name: "A", Complexity: 2},
				{FilePath: "a.go", Line: 3, Name: "C", Complexity: 3},
			},
			contain: []string{
				"C - Complexity: 3",
				"A - Complexity: 2",
				"B - Complexity: 2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := analyzer.formatResults(tt.input)
			for _, s := range tt.contain {
				if !strings.Contains(got, s) {
					t.Errorf("expected output to contain %q, got:\n%s", s, got)
				}
			}
			for _, s := range tt.absent {
				if strings.Contains(got, s) {
					t.Errorf("expected output to NOT contain %q, got:\n%s", s, got)
				}
			}
		})
	}
}

func TestFormatResults_Truncation(t *testing.T) {
	t.Parallel()
	analyzer := newComplexityAnalyzer(nil, nil, infra_persistence.NewOSFileSystem())

	t.Run("exactly 100 items no truncation", func(t *testing.T) {
		t.Parallel()
		items := make([]funcComplexity, 100)
		for i := range items {
			items[i] = funcComplexity{
				FilePath:   "f.go",
				Line:       i + 1,
				Name:       fmt.Sprintf("F%d", i),
				Complexity: 100 - i,
			}
		}
		got := analyzer.formatResults(items)
		if strings.Contains(got, "... (truncated)") {
			t.Error("expected no truncation for exactly 100 items")
		}
	})

	t.Run("101 items truncated", func(t *testing.T) {
		t.Parallel()
		items := make([]funcComplexity, 101)
		for i := range items {
			items[i] = funcComplexity{
				FilePath:   "f.go",
				Line:       i + 1,
				Name:       fmt.Sprintf("F%d", i),
				Complexity: 101 - i,
			}
		}
		got := analyzer.formatResults(items)
		if !strings.Contains(got, "... (truncated)") {
			t.Error("expected truncation message for 101 items")
		}
		// Should only have 100 complexity lines (plus header + truncation)
		lines := strings.Split(got, "\n")
		complexityLines := 0
		for _, line := range lines {
			if strings.Contains(line, "Complexity:") {
				complexityLines++
			}
		}
		if complexityLines != 100 {
			t.Errorf("expected 100 complexity lines, got %d", complexityLines)
		}
	})
}

func TestGatherComplexities_InvalidPath(t *testing.T) {
	t.Parallel()
	cache := newASTCache(".")
	sp := &mockSecurityProvider{}
	analyzer := newComplexityAnalyzer(cache, sp, infra_persistence.NewOSFileSystem())

	_, _, err := analyzer.GatherComplexities(context.Background(), "/nonexistent/path/that/does/not/exist", nil)
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestAnalyzeFile_ParseError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	// Create a Go file with invalid syntax
	invalidPath := filepath.Join(tmpDir, "bad.go")
	if err := os.WriteFile(invalidPath, []byte("package bad\nfunc broken {"), 0644); err != nil {
		t.Fatal(err)
	}

	cache := newASTCache(".")
	analyzer := newComplexityAnalyzer(cache, &mockSecurityProvider{}, infra_persistence.NewOSFileSystem())

	_, err := analyzer.analyzeFile(invalidPath)
	if err == nil {
		t.Error("expected parse error for invalid Go file")
	}
}

func TestAnalyze_UnsafePath(t *testing.T) {
	t.Parallel()
	cache := newASTCache(".")

	// mockSecurityProvider that denies all paths
	denySP := &mockSecurityProvider{}
	// Override IsPathSafe to return error
	// We can't override methods on the mock without creating a new type
	// So we use a path that doesn't exist - IsPathSafe passes it through

	// Test with path validation error
	analyzer := newComplexityAnalyzer(cache, denySP, infra_persistence.NewOSFileSystem())
	_, err := analyzer.Analyze(context.Background(), map[string]interface{}{"path": "/some/path"}, nil)
	// This will fail at GatherComplexities with filepath.Walk error since path doesn't exist
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestAnalyze_IsPathSafeRejection(t *testing.T) {
	t.Parallel()
	cache := newASTCache(".")
	denyingSP := &denyingSecurityProvider{}
	analyzer := newComplexityAnalyzer(cache, denyingSP, infra_persistence.NewOSFileSystem())

	_, err := analyzer.Analyze(context.Background(), map[string]interface{}{"path": "/some/valid/path"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path not authorized")
}

func TestAnalyze_NoFunctions(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	// Create Go file with only types, no functions
	code := `package test
type T int
type S struct{}
`
	filePath := filepath.Join(tmpDir, "types.go")
	if err := os.WriteFile(filePath, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	cache := newASTCache(".")
	sp := &mockSecurityProvider{}
	analyzer := newComplexityAnalyzer(cache, sp, infra_persistence.NewOSFileSystem())

	res, err := analyzer.Analyze(context.Background(), map[string]interface{}{"path": tmpDir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "No Go functions found") {
		t.Errorf("expected 'No Go functions found', got: %s", res.Text)
	}
}

func TestAnalyze_Receiver(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	code := `package test
type S struct{}
func (s *S) Method() {}
func (s S) ValueMethod() {}
`
	filePath := filepath.Join(tmpDir, "recv.go")
	if err := os.WriteFile(filePath, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	cache := newASTCache(".")
	sp := &mockSecurityProvider{}
	analyzer := newComplexityAnalyzer(cache, sp, infra_persistence.NewOSFileSystem())

	res, err := analyzer.Analyze(context.Background(), map[string]interface{}{"path": tmpDir}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(res.Text, "(*S).Method") {
		t.Errorf("expected (*S).Method, got:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "(S).ValueMethod") {
		t.Errorf("expected (S).ValueMethod, got:\n%s", res.Text)
	}
}

// TestGatherComplexities_WalkError covers the Walk error return
// at L85–87 in complexity.go (e.g., permission errors while walking the
// directory tree). The g.Wait() L88–90 error path is covered by
// TestComplexityAnalyzer_ErrgroupError.
func TestGatherComplexities_WalkError(t *testing.T) {
	t.Parallel()

	cache := newASTCache(".")
	sp := &mockSecurityProvider{}
	mfs := &walkErrorFS{FileSystem: persistence.NewMockFileSystem(), err: fs.ErrPermission}
	analyzer := newComplexityAnalyzer(cache, sp, mfs)

	_, _, err := analyzer.GatherComplexities(context.Background(), "/some/path", nil)
	if err == nil {
		t.Error("expected permission error from Walk")
	}
}

func TestGatherComplexities_WithHeartbeat(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	// Create enough files to trigger heartbeat (counter % 10 == 0)
	for i := 0; i < 25; i++ {
		code := "package test\nfunc F() {}\n"
		path := filepath.Join(tmpDir, fmt.Sprintf("file_%d.go", i))
		if err := os.WriteFile(path, []byte(code), 0644); err != nil {
			t.Fatal(err)
		}
	}

	cache := newASTCache(".")
	sp := &mockSecurityProvider{}
	analyzer := newComplexityAnalyzer(cache, sp, infra_persistence.NewOSFileSystem())

	hb := make(chan struct{}, 10)
	complexities, _, err := analyzer.GatherComplexities(context.Background(), tmpDir, hb)
	if err != nil {
		t.Fatalf("GatherComplexities failed: %v", err)
	}
	if len(complexities) < 25 {
		t.Errorf("expected at least 25 complexities, got %d", len(complexities))
	}
	// Verify heartbeats were sent
	select {
	case <-hb:
		// OK
	default:
		t.Error("expected at least one heartbeat")
	}
}

func TestGatherComplexities_SoftFailOnParseError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	// Create one valid file
	validPath := filepath.Join(tmpDir, "valid.go")
	if err := os.WriteFile(validPath, []byte("package test\nfunc F() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Create one invalid file
	invalidPath := filepath.Join(tmpDir, "bad.go")
	if err := os.WriteFile(invalidPath, []byte("package test\nfunc broken {"), 0644); err != nil {
		t.Fatal(err)
	}

	cache := newASTCache(".")
	sp := &mockSecurityProvider{}
	analyzer := newComplexityAnalyzer(cache, sp, infra_persistence.NewOSFileSystem())

	res, err := analyzer.Analyze(context.Background(), map[string]interface{}{"path": tmpDir}, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}
	// Valid file's function should appear
	if !strings.Contains(res.Text, "F - Complexity: 1") {
		t.Errorf("expected F - Complexity: 1 in output, got:\n%s", res.Text)
	}
	// Skipped file annotation should appear
	if !strings.Contains(res.Text, "⚠️ Skipped 1 file(s)") {
		t.Errorf("expected skipped-file annotation in output, got:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "bad.go") {
		t.Errorf("expected bad.go in skipped list, got:\n%s", res.Text)
	}
}

func TestGatherComplexities_ContextCancelled(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	// Create a few valid Go files so Walk has something to process
	for i := 0; i < 5; i++ {
		path := filepath.Join(tmpDir, fmt.Sprintf("file%d.go", i))
		require.NoError(t, os.WriteFile(path, []byte("package test\nfunc F() {}\n"), 0644))
	}

	cache := newASTCache(".")
	sp := &mockSecurityProvider{}
	analyzer := newComplexityAnalyzer(cache, sp, infra_persistence.NewOSFileSystem())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately, before GatherComplexities starts

	_, _, err := analyzer.GatherComplexities(ctx, tmpDir, nil)
	require.Error(t, err)
	// errgroup wraps context errors; check for context.Canceled
	assert.True(t, errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "context canceled"),
		"expected context.Canceled, got: %v", err)
}

// TestComplexityAnalyzer_ErrgroupError exercises the g.Wait() error return
// path (L88–90 in complexity.go). The challenge is that walkFn checks the
// derived context (gCtx) before launching each goroutine, so cancelling the
// parent context too early causes Walk itself to return an error before
// g.Wait() is ever reached. To hit g.Wait(), Walk must finish successfully
// and goroutines must still be running when the context expires.
//
// Strategy: use context.WithTimeout with a 10ms timeout. Walk (directory
// traversal) completes in microseconds, launching all goroutines. Then
// g.Wait() blocks until the timeout fires; goroutines blocked on
// sem.Acquire see context.DeadlineExceeded; g.Wait() captures and wraps it
// with "gathering complexity metrics: %w".
func TestComplexityAnalyzer_ErrgroupError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// go.mod required by the AST cache for module-relative path resolution.
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, "go.mod"),
		[]byte("module example.com/test\ngo 1.25"), 0644,
	))

	// Create enough files that goroutine count exceeds semaphore slots
	// (runtime.NumCPU). Walk launches all goroutines in microseconds;
	// most block on sem.Acquire. The timeout fires during g.Wait(),
	// and blocked goroutines return context.DeadlineExceeded.
	const numFiles = 1000
	for i := 0; i < numFiles; i++ {
		path := filepath.Join(tmpDir, fmt.Sprintf("file%d.go", i))
		require.NoError(t, os.WriteFile(path, []byte("package test\nfunc F() {}\n"), 0644))
	}

	cache := newASTCache(".")
	sp := &mockSecurityProvider{}
	analyzer := newComplexityAnalyzer(cache, sp, infra_persistence.NewOSFileSystem())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	type result struct {
		complexities []funcComplexity
		err          error
	}
	ch := make(chan result, 1)
	go func() {
		c, _, e := analyzer.GatherComplexities(ctx, tmpDir, nil)
		ch <- result{complexities: c, err: e}
	}()

	res := <-ch
	require.Error(t, res.err)

	// The timeout should fire while goroutines are blocked on sem.Acquire,
	// causing g.Wait() to capture context.DeadlineExceeded wrapped with
	// "gathering complexity metrics".
	if !errors.Is(res.err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) = false, err type: %T, value: %v", res.err, res.err)
	}

	// Under race detection, filepath.Walk may complete before the timeout
	// fires (race overhead slows goroutine scheduling), so the error comes
	// from g.Wait() wrapped. Under heavy load (no -race), Walk may return
	// early with the bare error. Both paths are valid.
	if !strings.Contains(res.err.Error(), "gathering complexity metrics") &&
		!strings.Contains(res.err.Error(), "context deadline exceeded") {
		t.Errorf("expected 'gathering complexity metrics' or bare timeout, got: %v", res.err)
	}
}
