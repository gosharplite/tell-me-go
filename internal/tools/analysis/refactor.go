package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
)

// transform represents a single code transformation.
type transform interface {
	Apply(ctx context.Context, fset *token.FileSet, files map[string]*ast.File) error
}

// transaction manages a set of transformations to be applied atomically.
type transaction struct {
	fset       *token.FileSet
	files      map[string]*ast.File
	transforms []transform
}

func newTransaction() *transaction {
	return &transaction{
		fset:  token.NewFileSet(),
		files: make(map[string]*ast.File),
	}
}

func (tx *transaction) Add(t transform) {
	tx.transforms = append(tx.transforms, t)
}

func (tx *transaction) Commit(ctx context.Context) error {
	for i, t := range tx.transforms {
		if err := t.Apply(ctx, tx.fset, tx.files); err != nil {
			return fmt.Errorf("transform %d: %w", i, err)
		}
	}

	var writtenFiles []string
	for path, file := range tx.files {
		tmpPath := path + ".tmp"
		f, err := os.Create(tmpPath)
		if err != nil {
			tx.rollback(writtenFiles)
			return fmt.Errorf("create temp file %s: %w", tmpPath, err)
		}
		if err := tx.safeFormat(f, file); err != nil {
			_ = f.Close()
			_ = os.Remove(tmpPath)
			tx.rollback(writtenFiles)
			return fmt.Errorf("format %s: %w", path, err)
		}
		_ = f.Close()
		writtenFiles = append(writtenFiles, path)
	}

	for i, path := range writtenFiles {
		if err := os.Rename(path+".tmp", path); err != nil {
			// Clean up remaining .tmp files from this point onward
			for _, p := range writtenFiles[i:] {
				_ = os.Remove(p + ".tmp")
			}
			return fmt.Errorf("failed to finalize %s: %w", path, err)
		}
	}

	return nil
}

// safeFormat calls format.Node with panic recovery to convert nil-deref
// panics (e.g. from nil ast.File.Name) into errors.
func (tx *transaction) safeFormat(w interface {
	Write([]byte) (int, error)
}, file *ast.File) (fmtErr error) {
	defer func() {
		if r := recover(); r != nil {
			fmtErr = fmt.Errorf("format.Node panic: %v", r)
		}
	}()
	return format.Node(w, tx.fset, file)
}

func (tx *transaction) rollback(writtenFiles []string) {
	for _, path := range writtenFiles {
		_ = os.Remove(path + ".tmp")
	}
}

func (tx *transaction) LoadFile(path string) (*ast.File, error) {
	if f, ok := tx.files[path]; ok {
		return f, nil
	}

	f, err := parser.ParseFile(tx.fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	tx.files[path] = f
	return f, nil
}
