package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangeAnalyzer_SemanticDiff(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a dummy file in the temp dir to represent the "current" state
	currCode := `package p
func NewFunc() {}
`
	relPath := "test.go"
	absPath := filepath.Join(tmpDir, relPath)
	if err := os.WriteFile(absPath, []byte(currCode), 0644); err != nil {
		t.Fatal(err)
	}

	mockExec := &MockExecutor{
		CombinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			switch args[0] {
			case "diff":
				if len(args) > 1 && args[1] == "--name-only" {
					return []byte(relPath), nil
				}
				return []byte("diff stat/summary"), nil
			}
			return nil, nil
		},
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if args[0] == "show" {
				// Base code (previous version)
				return []byte("package p\nfunc OldFunc() {}\n"), nil
			}
			return nil, nil
		},
	}

	cache := newASTCache()
	// We need to change directory to tmpDir so that cache.Get(relPath) finds the file
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Errorf("failed to restore working directory: %v", err)
		}
	}()

	analyzer := newChangeAnalyzer(cache, mockExec)
	res, err := analyzer.SemanticDiff(context.Background(), map[string]interface{}{"target": "HEAD~1"})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(res.Text, "Added: func NewFunc") {
		t.Errorf("Expected Added: func NewFunc, got:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "Deleted: func OldFunc") {
		t.Errorf("Expected Deleted: func OldFunc, got:\n%s", res.Text)
	}
}
