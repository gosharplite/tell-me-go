package analysis

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/imports"
)

func TestImportCleanupTransform_Apply(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "test.go",
			"package p\n\nimport \"fmt\"\n\nfunc main() { fmt.Println() }",
			parser.ParseComments)
		require.NoError(t, err)
		files := map[string]*ast.File{"test.go": file}
		tx := &importCleanupTransform{Path: "test.go"}
		err = tx.Apply(context.Background(), fset, files)
		require.NoError(t, err)
	})

	t.Run("file_not_loaded", func(t *testing.T) {
		t.Parallel()
		tx := &importCleanupTransform{Path: "unloaded.go"}
		err := tx.Apply(context.Background(), token.NewFileSet(), map[string]*ast.File{})
		require.NoError(t, err)
	})

	// Steps B (imports.Process) and C (parser.ParseFile) are defensive
	// branches that are exercised through the success path above. Both
	// are third-party functions (golang.org/x/tools/imports.Process
	// and go/parser.ParseFile) whose failure modes require environment-
	// specific conditions (import resolution infrastructure, corrupt
	// process output) that are impractical to simulate without override
	// variables. No such variables are introduced here; these branches
	// are covered by integration-level tests in CI.
	t.Run("format_Node_error", func(t *testing.T) {
		t.Parallel()
		formatErr := errors.New("format.Node failed: simulated write error")
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "test.go",
			"package p\n\nimport \"fmt\"\n\nfunc main() { fmt.Println() }",
			parser.ParseComments)
		require.NoError(t, err)
		files := map[string]*ast.File{"test.go": file}
		tx := &importCleanupTransform{
			Path: "test.go",
			testFormatNode: func(buf *bytes.Buffer, fset *token.FileSet, file *ast.File) error {
				return formatErr
			},
		}
		err = tx.Apply(context.Background(), fset, files)
		require.Error(t, err)
		require.ErrorIs(t, err, formatErr)
		if !strings.Contains(err.Error(), "formatting test.go") {
			t.Errorf("expected wrapping message 'formatting test.go', got: %v", err)
		}
	})
}

func TestImportCleanupTransform_Apply_ProcessError(t *testing.T) {
	t.Parallel()
	processErr := errors.New("imports.Process failed: GOROOT not found")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go",
		"package p\n\nimport \"fmt\"\n\nfunc main() { fmt.Println() }",
		parser.ParseComments)
	require.NoError(t, err)
	files := map[string]*ast.File{"test.go": file}
	tx := &importCleanupTransform{
		Path: "test.go",
		testProcessFunc: func(path string, src []byte, opts *imports.Options) ([]byte, error) {
			return nil, processErr
		},
	}
	err = tx.Apply(context.Background(), fset, files)
	require.Error(t, err)
	require.ErrorIs(t, err, processErr)
	if !strings.Contains(err.Error(), "processing imports for test.go") {
		t.Errorf("expected wrapping message, got: %v", err)
	}
}

func TestImportCleanupTransform_Apply_ParseError(t *testing.T) {
	t.Parallel()
	parseErr := errors.New("parser.ParseFile failed: invalid input")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go",
		"package p\n\nimport \"fmt\"\n\nfunc main() { fmt.Println() }",
		parser.ParseComments)
	require.NoError(t, err)
	files := map[string]*ast.File{"test.go": file}
	tx := &importCleanupTransform{
		Path: "test.go",
		testParseFileFunc: func(fset *token.FileSet, filename string, src interface{}, mode parser.Mode) (*ast.File, error) {
			return nil, parseErr
		},
	}
	err = tx.Apply(context.Background(), fset, files)
	require.Error(t, err)
	require.ErrorIs(t, err, parseErr)
	if !strings.Contains(err.Error(), "parsing formatted file test.go") {
		t.Errorf("expected wrapping message, got: %v", err)
	}
}

func TestFormatAndReprocess(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "test.go",
			"package p\n\nimport \"fmt\"\n\nfunc main() { fmt.Println() }",
			parser.ParseComments)
		require.NoError(t, err)

		tx := &importCleanupTransform{Path: "test.go"}
		newFile, err := tx.formatAndReprocess(fset, file)
		require.NoError(t, err)
		require.NotNil(t, newFile)
	})

	t.Run("format error via test hook", func(t *testing.T) {
		t.Parallel()
		formatErr := errors.New("format.Node failed: simulated write error")
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "test.go",
			"package p\n\nimport \"fmt\"\n\nfunc main() { fmt.Println() }",
			parser.ParseComments)
		require.NoError(t, err)

		tx := &importCleanupTransform{
			Path: "test.go",
			testFormatNode: func(buf *bytes.Buffer, fset *token.FileSet, file *ast.File) error {
				return formatErr
			},
		}
		_, err = tx.formatAndReprocess(fset, file)
		require.Error(t, err)
		require.ErrorIs(t, err, formatErr)
		if !strings.Contains(err.Error(), "formatting test.go") {
			t.Errorf("expected wrapping message 'formatting test.go', got: %v", err)
		}
	})

	t.Run("process error via test hook", func(t *testing.T) {
		t.Parallel()
		processErr := errors.New("imports.Process failed")
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "test.go",
			"package p\n\nimport \"fmt\"\n\nfunc main() { fmt.Println() }",
			parser.ParseComments)
		require.NoError(t, err)

		tx := &importCleanupTransform{
			Path: "test.go",
			testProcessFunc: func(path string, src []byte, opts *imports.Options) ([]byte, error) {
				return nil, processErr
			},
		}
		_, err = tx.formatAndReprocess(fset, file)
		require.Error(t, err)
		if !strings.Contains(err.Error(), "processing imports for test.go") {
			t.Errorf("expected wrapping message, got: %v", err)
		}
	})

	t.Run("parse error via test hook", func(t *testing.T) {
		t.Parallel()
		parseErr := errors.New("parser.ParseFile failed")
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "test.go",
			"package p\n\nimport \"fmt\"\n\nfunc main() { fmt.Println() }",
			parser.ParseComments)
		require.NoError(t, err)

		tx := &importCleanupTransform{
			Path: "test.go",
			testParseFileFunc: func(fset *token.FileSet, filename string, src interface{}, mode parser.Mode) (*ast.File, error) {
				return nil, parseErr
			},
		}
		_, err = tx.formatAndReprocess(fset, file)
		require.Error(t, err)
		if !strings.Contains(err.Error(), "parsing formatted file test.go") {
			t.Errorf("expected wrapping message, got: %v", err)
		}
	})
}
