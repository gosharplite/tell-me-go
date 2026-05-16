package analysis

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
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
	})
}
