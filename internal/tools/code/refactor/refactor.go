package refactor

import (
	"context"
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

	for path, file := range tx.files {
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		if err := format.Node(f, tx.fset, file); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}

	return nil
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
