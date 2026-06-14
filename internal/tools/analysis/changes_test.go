package analysis

import (
	"context"
	"errors"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChangeAnalyzer_SemanticDiff(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	relPath := "test.go"
	setupSemanticDiffFile(t, tmpDir, relPath)

	mockExec := setupSemanticDiffMock(relPath)
	cache := newASTCache(tmpDir)

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

func assertSemanticDiffResult(t *testing.T, text string) {
	if !strings.Contains(text, "Added: func NewFunc") {
		t.Errorf("Expected Added: func NewFunc, got:\n%s", text)
	}
	if !strings.Contains(text, "Deleted: func OldFunc") {
		t.Errorf("Expected Deleted: func OldFunc, got:\n%s", text)
	}
}

func TestChangeAnalyzer_SemanticDiff_NameOnlyError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	relPath := "test.go"
	setupSemanticDiffFile(t, tmpDir, relPath)

	mockExec := &mockExecutor{
		CombinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if len(args) > 1 && args[1] == "--name-only" {
				return nil, errors.New("git --name-only failed")
			}
			return []byte("some stat"), nil
		},
	}
	cache := newASTCache(tmpDir)
	analyzer := newChangeAnalyzer(cache, mockExec)

	res, err := analyzer.SemanticDiff(context.Background(), map[string]interface{}{"target": "HEAD~1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "Could not perform logical analysis") {
		t.Errorf("expected soft-error message, got:\n%s", res.Text)
	}
}

func TestChangeAnalyzer_SemanticDiff_StatSummaryErrors(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	relPath := "test.go"
	setupSemanticDiffFile(t, tmpDir, relPath)

	mockExec := &mockExecutor{
		CombinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if len(args) > 1 && args[1] == "--stat" {
				return nil, errors.New("stat error")
			}
			if len(args) > 1 && args[1] == "--summary" {
				return nil, errors.New("summary error")
			}
			if len(args) > 1 && args[1] == "--name-only" {
				return []byte(relPath), nil
			}
			return nil, nil
		},
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("package p\nfunc OldFunc() {}\n"), nil
		},
	}
	cache := newASTCache(tmpDir)
	analyzer := newChangeAnalyzer(cache, mockExec)

	res, err := analyzer.SemanticDiff(context.Background(), map[string]interface{}{"target": "HEAD~1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "stat error") {
		t.Errorf("expected stat error in output, got:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "summary error") {
		t.Errorf("expected summary error in output, got:\n%s", res.Text)
	}
}

func TestAnalyzeFileChange_NewFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	relPath := "newfile.go"
	code := `package p
func BrandNew() {}
type NewType struct{}
`
	absPath := filepath.Join(tmpDir, relPath)
	if err := os.WriteFile(absPath, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	// Mock git show returning error (file doesn't exist in target)
	mockExec := &mockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, errors.New("fatal: path 'newfile.go' does not exist in 'HEAD~1'")
		},
	}
	cache := newASTCache(tmpDir)
	analyzer := newChangeAnalyzer(cache, mockExec)

	fset := token.NewFileSet()
	changes, err := analyzer.analyzeFileChange(context.Background(), "HEAD~1", absPath, fset)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %v", len(changes), changes)
	}
	if !strings.Contains(changes[0], "Added: func BrandNew") {
		t.Errorf("expected Added: func BrandNew, got %q", changes[0])
	}
}

func TestAnalyzeFileChange_UnparseableBase(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	relPath := "file.go"
	code := `package p
func CurrentFunc() {}
`
	absPath := filepath.Join(tmpDir, relPath)
	if err := os.WriteFile(absPath, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	// Mock git show returning unparseable Go code
	mockExec := &mockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("not valid go code {{{"), nil
		},
	}
	cache := newASTCache(tmpDir)
	analyzer := newChangeAnalyzer(cache, mockExec)

	fset := token.NewFileSet()
	_, err := analyzer.analyzeFileChange(context.Background(), "HEAD~1", absPath, fset)
	if err == nil {
		t.Error("expected error for unparseable base")
	}
	if !strings.Contains(err.Error(), "could not analyze base version") {
		t.Errorf("expected 'could not analyze base' error, got %v", err)
	}
}

func TestAnalyzeFileChange_CurrentParseError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	invalidPath := filepath.Join(tmpDir, "bad.go")
	if err := os.WriteFile(invalidPath, []byte("not valid go"), 0644); err != nil {
		t.Fatal(err)
	}

	mockExec := &mockExecutor{}
	cache := newASTCache(tmpDir)
	analyzer := newChangeAnalyzer(cache, mockExec)

	fset := token.NewFileSet()
	_, err := analyzer.analyzeFileChange(context.Background(), "HEAD~1", invalidPath, fset)
	if err == nil {
		t.Error("expected parse error for invalid current file")
	}
}

func TestGetDiffMetadata_EmptyChangeSet(t *testing.T) {
	t.Parallel()
	mockExec := &mockExecutor{
		CombinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if len(args) > 1 && args[1] == "--name-only" {
				return []byte(""), nil
			}
			return []byte("no changes"), nil
		},
	}
	analyzer := newChangeAnalyzer(nil, mockExec)

	metadata, changedFiles, err := analyzer.getDiffMetadata(context.Background(), "HEAD~1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changedFiles != nil {
		t.Errorf("expected nil changedFiles for empty diff, got %v", changedFiles)
	}
	_ = metadata
}

func TestSemanticDiff_AnalyzeFileChangeError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	relPath := "file.go"
	// Create a valid Go file that exists in current, but mock git show returns unparseable code
	code := `package p
func CurrentFunc() {}
`
	absPath := filepath.Join(tmpDir, relPath)
	if err := os.WriteFile(absPath, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	mockExec := &mockExecutor{
		CombinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if len(args) > 1 {
				switch args[1] {
				case "--stat":
					return []byte("stat output"), nil
				case "--summary":
					return []byte("summary output"), nil
				case "--name-only":
					return []byte(relPath), nil
				}
			}
			return nil, nil
		},
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			// Return unparseable Go code for the base version
			return []byte("not valid go code {{{"), nil
		},
	}

	cache := newASTCache(tmpDir)
	analyzer := newChangeAnalyzer(cache, mockExec)

	res, err := analyzer.SemanticDiff(context.Background(), map[string]interface{}{"target": "HEAD~1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Should contain the analysis error as a soft-error
	if !strings.Contains(res.Text, "analysis error") {
		t.Errorf("expected analysis error in output, got:\n%s", res.Text)
	}
}

func TestGetDiffMetadata_StatError(t *testing.T) {
	t.Parallel()
	mockExec := &mockExecutor{
		CombinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if len(args) > 1 && args[1] == "--stat" {
				return nil, errors.New("stat failed")
			}
			if len(args) > 1 && args[1] == "--summary" {
				return []byte("summary"), nil
			}
			if len(args) > 1 && args[1] == "--name-only" {
				return []byte("file.go"), nil
			}
			return nil, nil
		},
	}
	analyzer := newChangeAnalyzer(nil, mockExec)

	metadata, changedFiles, err := analyzer.getDiffMetadata(context.Background(), "HEAD~1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(metadata, "stat failed") {
		t.Errorf("expected stat error in metadata, got: %s", metadata)
	}
	if len(changedFiles) != 1 {
		t.Errorf("expected 1 changed file, got %d", len(changedFiles))
	}
}

func TestSemanticDiff_UnmarshalArgsError(t *testing.T) {
	t.Parallel()
	analyzer := newChangeAnalyzer(nil, nil)

	t.Run("nil args", func(t *testing.T) {
		t.Parallel()
		_, err := analyzer.SemanticDiff(context.Background(), nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "args must not be nil")
	})

	t.Run("missing target", func(t *testing.T) {
		t.Parallel()
		_, err := analyzer.SemanticDiff(context.Background(), map[string]interface{}{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing required argument: target")
	})

	t.Run("target wrong type wraps with semantic diff context", func(t *testing.T) {
		t.Parallel()
		_, err := analyzer.SemanticDiff(context.Background(), map[string]interface{}{"target": 42}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "semantic diff")
	})
}

func TestSemanticDiff_InvalidTarget(t *testing.T) {
	t.Parallel()
	mockExec := &mockExecutor{
		CombinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, fmt.Errorf("fatal: ambiguous argument 'nonexistent-commit-hash-12345': unknown revision")
		},
	}
	a := newChangeAnalyzer(nil, mockExec)
	res, err := a.SemanticDiff(context.Background(), map[string]interface{}{
		"target": "nonexistent-commit-hash-12345",
	}, nil)
	// SemanticDiff handles getDiffMetadata errors gracefully — returns a result
	// with the error embedded in the text, not as a returned error.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "Could not perform logical analysis") {
		t.Errorf("expected soft-error message, got:\n%s", res.Text)
	}
}
