package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangeAnalyzer_SemanticDiff(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	relPath := "test.go"
	setupSemanticDiffFile(t, tmpDir, relPath)

	mockExec := setupSemanticDiffMock(relPath)
	cache := newASTCache()

	oldDir := changeToTempDir(t, tmpDir)
	defer restoreDir(t, oldDir)

	analyzer := newChangeAnalyzer(cache, mockExec)
	res, err := analyzer.SemanticDiff(context.Background(), map[string]interface{}{"target": "HEAD~1"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	assertSemanticDiffResult(t, res.Text)
}

func setupSemanticDiffFile(t *testing.T, tmpDir, relPath string) {
	currCode := `package p
func NewFunc() {}
`
	absPath := filepath.Join(tmpDir, relPath)
	if err := os.WriteFile(absPath, []byte(currCode), 0644); err != nil {
		t.Fatal(err)
	}
}

func setupSemanticDiffMock(relPath string) *mockExecutor {
	return &mockExecutor{
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
				return []byte("package p\nfunc OldFunc() {}\n"), nil
			}
			return nil, nil
		},
	}
}

func changeToTempDir(t *testing.T, tmpDir string) string {
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	return oldDir
}

func restoreDir(t *testing.T, oldDir string) {
	if err := os.Chdir(oldDir); err != nil {
		t.Errorf("failed to restore working directory: %v", err)
	}
}

func assertSemanticDiffResult(t *testing.T, text string) {
	if !strings.Contains(text, "Added: func NewFunc") {
		t.Errorf("Expected Added: func NewFunc, got:\n%s", text)
	}
	if !strings.Contains(text, "Deleted: func OldFunc") {
		t.Errorf("Expected Deleted: func OldFunc, got:\n%s", text)
	}
}
