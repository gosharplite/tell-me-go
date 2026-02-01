package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools/code/astutil"
)

func TestComplexityAnalyzer_Analyze(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "complexity-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

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

	cache := astutil.NewASTCache()
	sp := &mockSecurityProvider{}
	analyzer := NewComplexityAnalyzer(cache, sp)

	res, err := analyzer.Analyze(context.Background(), map[string]interface{}{"path": tmpDir})
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if !testing.Short() {
		t.Logf("Result:\n%s", res.Text)
	}

	if !strings.Contains(res.Text, "Simple - Complexity: 1") {
		t.Errorf("Expected Simple to have complexity 1")
	}
	if !strings.Contains(res.Text, "Complex - Complexity: 3") {
		t.Errorf("Expected Complex to have complexity 3")
	}
}
