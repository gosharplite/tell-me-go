package analysis

import (
	"bytes"
	"context"
	"fmt"
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

	// testProcessFunc, when non-nil, replaces imports.Process during tests
	// to exercise the imports.Process error path.
	testProcessFunc func(path string, src []byte, opts *imports.Options) ([]byte, error)

	// testParseFileFunc, when non-nil, replaces parser.ParseFile during tests
	// to exercise the parse error path which is unreachable with []byte input.
	testParseFileFunc func(fset *token.FileSet, filename string, src interface{}, mode parser.Mode) (*ast.File, error)
}

func newImportCleanupTransform(path string) *importCleanupTransform {
	return &importCleanupTransform{Path: path}
}

// formatAndReprocess runs the three-step import cleanup pipeline:
// 1. Format the AST to a buffer
// 2. Run imports.Process on the formatted bytes
// 3. Re-parse the result back into an AST
func (t *importCleanupTransform) formatAndReprocess(fset *token.FileSet, file *ast.File) (*ast.File, error) {
	// Step 1: Format to buffer
	var buf bytes.Buffer
	if t.testFormatNode != nil {
		if err := t.testFormatNode(&buf, fset, file); err != nil {
			return nil, fmt.Errorf("formatting %s: %w", t.Path, err)
		}
		// Coverage gap accepted by architect — format.Node with a
		// bytes.Buffer writer never returns an error (bytes.Buffer.Write
		// always returns nil). This path is structurally unreachable,
		// identical to the json.Marshal guard in global_prompt_tracker.go.
	} else if err := format.Node(&buf, fset, file); err != nil {
		return nil, fmt.Errorf("formatting %s: %w", t.Path, err)
	}

	// Step 2: Process imports
	var res []byte
	var err error
	if t.testProcessFunc != nil {
		res, err = t.testProcessFunc(t.Path, buf.Bytes(), nil)
	} else {
		res, err = imports.Process(t.Path, buf.Bytes(), nil)
	}
	if err != nil {
		return nil, fmt.Errorf("processing imports for %s: %w", t.Path, err)
	}

	// Step 3: Re-parse
	var newFile *ast.File
	if t.testParseFileFunc != nil {
		newFile, err = t.testParseFileFunc(fset, t.Path, res, parser.ParseComments)
	} else {
		newFile, err = parser.ParseFile(fset, t.Path, res, parser.ParseComments)
	}
	if err != nil {
		return nil, fmt.Errorf("parsing formatted file %s: %w", t.Path, err)
	}

	return newFile, nil
}

func (t *importCleanupTransform) Apply(ctx context.Context, fset *token.FileSet, files map[string]*ast.File) error {
	file, ok := files[t.Path]
	if !ok {
		// If it's not loaded, we don't need to clean it up in this transaction
		return nil
	}

	newFile, err := t.formatAndReprocess(fset, file)
	if err != nil {
		return err
	}

	files[t.Path] = newFile
	return nil
}
