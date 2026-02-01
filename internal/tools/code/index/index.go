package index

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/tools/code/astutil"
	"golang.org/x/tools/go/packages"
)

// Location represents a position in a source file.
type Location struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// SymbolLocation extends Location with symbol metadata.
type SymbolLocation struct {
	Location
	Name      string `json:"name"`
	Kind      string `json:"kind"` // "func", "type", "var", "const"
	Signature string `json:"signature,omitempty"`
	Receiver  string `json:"receiver,omitempty"` // For methods
}

// TypeName represents a fully qualified type name.
type TypeName struct {
	PkgPath string `json:"pkg_path"`
	Name    string `json:"name"`
}

// SymbolIndex provides methods to query symbols and their relationships in a Go workspace.
type SymbolIndex interface {
	// Lookup returns the locations where the given symbol is defined.
	Lookup(ctx context.Context, symbol string) ([]Location, error)
	// FindImplementors returns the types that implement the given interface.
	FindImplementors(ctx context.Context, interfaceName string) ([]TypeName, error)
	// SearchSymbols searches for symbols matching the query in the given path.
	SearchSymbols(ctx context.Context, path string, query string, exportedOnly bool) ([]SymbolLocation, error)
	// GetUsages returns all locations where the given symbol name is used.
	GetUsages(ctx context.Context, symbol string, path string) ([]Location, error)
	// Refresh re-scans the workspace to update the index.
	Refresh(ctx context.Context) error
}

// Indexer implements SymbolIndex using go/packages and go/types.
type Indexer struct {
	dir  string
	fset *token.FileSet
	mu   sync.RWMutex
	pkgs []*packages.Package

	symbolsByPath map[string][]SymbolLocation
	usagesByName  map[string][]Location
	lastRefresh   time.Time
	refreshMu     sync.Mutex // For serializing Refresh calls
}

const refreshTTL = 5 * time.Second

func NewIndexer(dir string) (*Indexer, error) {
	return &Indexer{
		dir:           dir,
		fset:          token.NewFileSet(),
		symbolsByPath: make(map[string][]SymbolLocation),
		usagesByName:  make(map[string][]Location),
	}, nil
}

func (idx *Indexer) Refresh(ctx context.Context) error {
	idx.mu.RLock()
	needsRefresh := time.Since(idx.lastRefresh) > refreshTTL
	idx.mu.RUnlock()

	if !needsRefresh {
		return nil
	}

	// Serialize concurrent refresh attempts
	idx.refreshMu.Lock()
	defer idx.refreshMu.Unlock()

	// Double check after acquiring lock
	if time.Since(idx.lastRefresh) <= refreshTTL {
		return nil
	}

	// Build new index in local variables to avoid mid-scan empty results
	newSymbolsByPath := make(map[string][]SymbolLocation)
	newUsagesByName := make(map[string][]Location)

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
		for _, file := range pkg.Syntax {
			filename := idx.fset.File(file.Pos()).Name()
			absPath, _ := filepath.Abs(filename)

			ast.Inspect(file, func(n ast.Node) bool {
				if n == nil {
					return true
				}

				switch d := n.(type) {
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						switch s := spec.(type) {
						case *ast.ValueSpec:
							kind := "var"
							if d.Tok == token.CONST {
								kind = "const"
							}
							for _, name := range s.Names {
								loc := idx.toLocation(name.Pos())
								newSymbolsByPath[absPath] = append(newSymbolsByPath[absPath], SymbolLocation{
									Location: loc,
									Name:     name.Name,
									Kind:     kind,
								})
							}
						case *ast.TypeSpec:
							loc := idx.toLocation(s.Name.Pos())
							newSymbolsByPath[absPath] = append(newSymbolsByPath[absPath], SymbolLocation{
								Location: loc,
								Name:     s.Name.Name,
								Kind:     "type",
							})
						}
					}
				case *ast.FuncDecl:
					kind := "func"
					sig := astutil.GetFuncSignature(d)
					recv := ""
					if d.Recv != nil && len(d.Recv.List) > 0 {
						recv = astutil.ExprToString(d.Recv.List[0].Type)
					}
					loc := idx.toLocation(d.Name.Pos())
					newSymbolsByPath[absPath] = append(newSymbolsByPath[absPath], SymbolLocation{
						Location:  loc,
						Name:      d.Name.Name,
						Kind:      kind,
						Signature: sig,
						Receiver:  recv,
					})
				case *ast.Ident:
					loc := idx.toLocation(d.Pos())
					newUsagesByName[d.Name] = append(newUsagesByName[d.Name], loc)
				}
				return true
			})
		}
	}

	// Atomic swap
	idx.mu.Lock()
	idx.pkgs = pkgs
	idx.symbolsByPath = newSymbolsByPath
	idx.usagesByName = newUsagesByName
	idx.lastRefresh = time.Now()
	idx.mu.Unlock()

	return nil
}

func (idx *Indexer) toLocation(pos token.Pos) Location {
	p := idx.fset.Position(pos)
	abs, _ := filepath.Abs(p.Filename)
	return Location{
		Path:   abs,
		Line:   p.Line,
		Column: p.Column,
	}
}

func (idx *Indexer) Lookup(ctx context.Context, symbol string) ([]Location, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var locations []Location
	for _, pkg := range idx.pkgs {
		obj := pkg.Types.Scope().Lookup(symbol)
		if obj != nil {
			locations = append(locations, idx.toLocation(obj.Pos()))
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

func (idx *Indexer) SearchSymbols(ctx context.Context, path string, query string, exportedOnly bool) ([]SymbolLocation, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	searchPath, err := filepath.Abs(path)
	if err != nil {
		searchPath = path
	}

	var results []SymbolLocation
	query = strings.ToLower(query)

	for p, syms := range idx.symbolsByPath {
		rel, err := filepath.Rel(searchPath, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}

		for _, sym := range syms {
			if exportedOnly && !ast.IsExported(sym.Name) {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(sym.Name), query) {
				continue
			}
			results = append(results, sym)
		}
	}
	return results, nil
}

func (idx *Indexer) GetUsages(ctx context.Context, symbol string, path string) ([]Location, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	searchPath, err := filepath.Abs(path)
	if err != nil {
		searchPath = path
	}

	var results []Location
	usages, ok := idx.usagesByName[symbol]
	if !ok {
		return nil, nil
	}

	for _, loc := range usages {
		rel, err := filepath.Rel(searchPath, loc.Path)
		if err == nil && !strings.HasPrefix(rel, "..") {
			results = append(results, loc)
		}
	}

	return results, nil
}
