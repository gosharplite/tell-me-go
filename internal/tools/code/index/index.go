package index

import (
	"context"
	"fmt"
	"go/token"
	"go/types"
	"sync"

	"golang.org/x/tools/go/packages"
)

// Location represents a position in a source file.
type Location struct {
	Path   string
	Line   int
	Column int
}

// TypeName represents a fully qualified type name.
type TypeName struct {
	PkgPath string
	Name    string
}

// SymbolIndex provides methods to query symbols and their relationships in a Go workspace.
type SymbolIndex interface {
	// Lookup returns the locations where the given symbol is defined.
	Lookup(ctx context.Context, symbol string) ([]Location, error)
	// FindImplementors returns the types that implement the given interface.
	FindImplementors(ctx context.Context, interfaceName string) ([]TypeName, error)
	// Refresh re-scans the workspace to update the index.
	Refresh(ctx context.Context) error
}

// Indexer implements SymbolIndex using go/packages and go/types.
type Indexer struct {
	dir  string
	fset *token.FileSet
	mu   sync.RWMutex
	pkgs []*packages.Package
}

func NewIndexer(dir string) (*Indexer, error) {
	return &Indexer{
		dir:  dir,
		fset: token.NewFileSet(),
	}, nil
}

func (idx *Indexer) Refresh(ctx context.Context) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if idx.pkgs != nil {
		// For now, simple "once per session" caching
		// In a long-running service, we'd check file mod times
		return nil
	}
	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
		Dir:     idx.dir,
		Fset:    idx.fset,
		Context: ctx,
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return err
	}

	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return pkg.Errors[0]
		}
	}

	idx.pkgs = pkgs
	return nil
}

func (idx *Indexer) Lookup(ctx context.Context, symbol string) ([]Location, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var locations []Location
	for _, pkg := range idx.pkgs {
		obj := pkg.Types.Scope().Lookup(symbol)
		if obj != nil {
			pos := idx.fset.Position(obj.Pos())
			locations = append(locations, Location{
				Path:   pos.Filename,
				Line:   pos.Line,
				Column: pos.Column,
			})
		}
	}
	return locations, nil
}

func (idx *Indexer) FindImplementors(ctx context.Context, interfaceName string) ([]TypeName, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var iface *types.Interface

	// Find the interface type
	for _, pkg := range idx.pkgs {
		obj := pkg.Types.Scope().Lookup(interfaceName)
		if obj == nil {
			continue
		}
		if t, ok := obj.Type().Underlying().(*types.Interface); ok {
			iface = t
			break
		}
	}

	if iface == nil {
		return nil, fmt.Errorf("interface %s not found", interfaceName)
	}

	var implementors []TypeName
	for _, pkg := range idx.pkgs {
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if _, ok := obj.Type().Underlying().(*types.Interface); ok {
				continue
			}

			if types.Implements(obj.Type(), iface) || types.Implements(types.NewPointer(obj.Type()), iface) {
				implementors = append(implementors, TypeName{
					PkgPath: pkg.PkgPath,
					Name:    name,
				})
			}
		}
	}

	return implementors, nil
}
