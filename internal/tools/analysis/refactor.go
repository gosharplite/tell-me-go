package analysis

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
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
	fs         persistence.FileSystem
}

// newTransactionWithFS constructs a transaction bound to an explicitly
// injected filesystem. nil is a contract violation: the package has no
// default fallback — the ADR-055 defaultFS shim was retired (#1465), so
// every caller (the DI hub in production, tests elsewhere) must provide a
// non-nil persistence.FileSystem. The panic guards test-reachable seams
// and misuse.
func newTransactionWithFS(fs persistence.FileSystem) *transaction {
	if fs == nil {
		panic("nil FileSystem to newTransactionWithFS: inject a non-nil persistence.FileSystem (no package default exists)")
	}
	return &transaction{
		fset:  token.NewFileSet(),
		files: make(map[string]*ast.File),
		fs:    fs,
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

	for path, file := range tx.files {
		var buf bytes.Buffer
		if err := tx.safeFormat(&buf, file); err != nil {
			return fmt.Errorf("format %s: %w", path, err)
		}
		if err := tx.fs.AtomicWrite(ctx, path, buf.Bytes(), 0644); err != nil {
			return fmt.Errorf("atomic write %s: %w", path, err)
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

func (tx *transaction) LoadFile(ctx context.Context, path string) (*ast.File, error) {
	if f, ok := tx.files[path]; ok {
		return f, nil
	}

	data, err := tx.fs.ReadFile(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	f, err := parser.ParseFile(tx.fset, path, data, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	tx.files[path] = f
	return f, nil
}
