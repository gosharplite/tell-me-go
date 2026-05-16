package analysis

import (
	"bytes"
	"context"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"

	"golang.org/x/tools/imports"
)

type importCleanupTransform struct {
	Path string

	// testFormatNode, when non-nil, replaces format.Node during tests
	// to exercise the format error path which is unreachable with bytes.Buffer.
	testFormatNode func(buf *bytes.Buffer, fset *token.FileSet, file *ast.File) error
}

func newImportCleanupTransform(path string) *importCleanupTransform {
	return &importCleanupTransform{Path: path}
}

func (t *importCleanupTransform) Apply(ctx context.Context, fset *token.FileSet, files map[string]*ast.File) error {
	file, ok := files[t.Path]
	if !ok {
		// If it's not loaded, we don't need to clean it up in this transaction
		return nil
	}

	// Clean up imports using imports.Process
	// We need to format the file to a buffer first because imports.Process works on bytes
	var buf bytes.Buffer
	if t.testFormatNode != nil {
		if err := t.testFormatNode(&buf, fset, file); err != nil {
			return err
		}
	} else if err := format.Node(&buf, fset, file); err != nil {
		return err
	}

	res, err := imports.Process(t.Path, buf.Bytes(), nil)
	if err != nil {
		return err
	}

	// Re-parse the result back into the AST
	newFile, err := parser.ParseFile(fset, t.Path, res, parser.ParseComments)
	if err != nil {
		return err
	}

	files[t.Path] = newFile
	return nil
}
