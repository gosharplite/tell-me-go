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
	for _, t := range tx.transforms {
		if err := t.Apply(ctx, tx.fset, tx.files); err != nil {
			return err
		}
	}

	var writtenFiles []string
	for path, file := range tx.files {
		tmpPath := path + ".tmp"
		f, err := os.Create(tmpPath)
		if err != nil {
			tx.rollback(writtenFiles)
			return err
		}
		if err := format.Node(f, tx.fset, file); err != nil {
			_ = f.Close()
			_ = os.Remove(tmpPath)
			tx.rollback(writtenFiles)
			return err
		}
		_ = f.Close()
		writtenFiles = append(writtenFiles, path)
	}

	for _, path := range writtenFiles {
		if err := os.Rename(path+".tmp", path); err != nil {
			// This is tricky if it fails mid-way, but Renaming is usually atomic on the same FS
			return fmt.Errorf("failed to finalize %s: %w", path, err)
		}
	}

	return nil
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
		return nil, err
	}

	tx.files[path] = f
	return f, nil
}
