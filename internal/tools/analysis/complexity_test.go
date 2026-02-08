package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComplexityAnalyzer_Analyze(t *testing.T) {
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

	cache := NewASTCache()
	sp := &mockSecurityProvider{}
	analyzer := NewComplexityAnalyzer(cache, sp)

	res, err := analyzer.Analyze(context.Background(), map[string]interface{}{"path": tmpDir})
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

	cache := NewASTCache()
	sp := &mockSecurityProvider{}
	analyzer := NewComplexityAnalyzer(cache, sp)

	res, err := analyzer.Analyze(context.Background(), map[string]interface{}{"path": tmpDir})
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
