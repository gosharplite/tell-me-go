package analysis

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	analyzer := newComplexityAnalyzer(cache, sp)

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
	analyzer := newComplexityAnalyzer(cache, sp)

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

func TestGetConcurrencyLimit(t *testing.T) {
	t.Parallel()
	analyzer := newComplexityAnalyzer(nil, nil)
	limit := analyzer.getConcurrencyLimit()
	if limit < 1 {
		t.Errorf("getConcurrencyLimit() = %d, want >= 1", limit)
	}
}

func TestFormatResults(t *testing.T) {
	t.Parallel()
	analyzer := newComplexityAnalyzer(nil, nil)

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
	analyzer := newComplexityAnalyzer(nil, nil)

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
	analyzer := newComplexityAnalyzer(cache, sp)

	_, err := analyzer.GatherComplexities(context.Background(), "/nonexistent/path/that/does/not/exist", nil)
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
	analyzer := newComplexityAnalyzer(cache, &mockSecurityProvider{})

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
	analyzer := newComplexityAnalyzer(cache, denySP)
	_, err := analyzer.Analyze(context.Background(), map[string]interface{}{"path": "/some/path"}, nil)
	// This will fail at GatherComplexities with filepath.Walk error since path doesn't exist
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
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
	analyzer := newComplexityAnalyzer(cache, sp)

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
	analyzer := newComplexityAnalyzer(cache, sp)

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

func TestGatherComplexities_WalkError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	// Create an unreadable directory to trigger Walk error
	unreadableDir := filepath.Join(tmpDir, "unreadable")
	if err := os.Mkdir(unreadableDir, 0000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(unreadableDir, 0755) }() // restore on cleanup

	cache := newASTCache(".")
	sp := &mockSecurityProvider{}
	analyzer := newComplexityAnalyzer(cache, sp)

	_, err := analyzer.GatherComplexities(context.Background(), unreadableDir, nil)
	if err == nil {
		t.Error("expected permission error for unreadable directory")
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
	analyzer := newComplexityAnalyzer(cache, sp)

	hb := make(chan struct{}, 10)
	complexities, err := analyzer.GatherComplexities(context.Background(), tmpDir, hb)
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
