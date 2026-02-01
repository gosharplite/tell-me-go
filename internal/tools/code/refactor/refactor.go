package refactor

import (
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
)

// Transform represents a single code transformation.
type Transform interface {
	Apply(ctx context.Context, fset *token.FileSet, files map[string]*ast.File) error
}

// Transaction manages a set of transformations to be applied atomically.
type Transaction struct {
	fset       *token.FileSet
	files      map[string]*ast.File
	transforms []Transform
}

func NewTransaction() *Transaction {
	return &Transaction{
		fset:  token.NewFileSet(),
		files: make(map[string]*ast.File),
	}
}

func (tx *Transaction) Add(t Transform) {
	tx.transforms = append(tx.transforms, t)
}

func (tx *Transaction) Commit(ctx context.Context) error {
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
			f.Close()
			os.Remove(tmpPath)
			tx.rollback(writtenFiles)
			return err
		}
		f.Close()
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

func (tx *Transaction) rollback(writtenFiles []string) {
	for _, path := range writtenFiles {
		os.Remove(path + ".tmp")
	}
}

func (tx *Transaction) LoadFile(path string) (*ast.File, error) {
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
