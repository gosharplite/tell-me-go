package analysis

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

	"golang.org/x/tools/go/packages"
)

// location represents a position in a source file.
type location struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// symbolLocation extends location with symbol metadata.
type symbolLocation struct {
	location
	Name      string `json:"name"`
	Kind      string `json:"kind"` // "func", "type", "var", "const"
	Signature string `json:"signature,omitempty"`
	Receiver  string `json:"receiver,omitempty"` // For methods
}

// typeName represents a fully qualified type name.
type typeName struct {
	PkgPath string `json:"pkg_path"`
	Name    string `json:"name"`
}

// SymbolIndex provides methods to query symbols and their relationships in a Go workspace.
type symbolIndex interface {
	// Lookup returns the locations where the given symbol is defined.
	Lookup(ctx context.Context, symbol string) ([]location, error)
	// FindImplementors returns the types that implement the given interface.
	FindImplementors(ctx context.Context, interfaceName string) ([]typeName, error)
	// SearchSymbols searches for symbols matching the query in the given path.
	SearchSymbols(ctx context.Context, path string, query string, exportedOnly bool) ([]symbolLocation, error)
	// GetUsages returns all locations where the given symbol name is used.
	GetUsages(ctx context.Context, symbol string, path string) ([]location, error)
	// IsSymbolUsed returns true if the provided name exists in the index with at least one usage.
	IsSymbolUsed(name string) bool
	// GetImplementations returns the concrete method identities that implement the given interface method.
	GetImplementations(interfaceMethodId string) []string
	// Packages returns the loaded packages.
	Packages() []*packages.Package
	// Refresh re-scans the workspace to update the index.
	Refresh(ctx context.Context) error
}

// Indexer implements SymbolIndex using go/packages and go/types.
type indexer struct {
	dir  string
	fset *token.FileSet
	mu   sync.RWMutex
	pkgs []*packages.Package

	symbolsByPath   map[string][]symbolLocation
	usagesByName    map[string][]location
	implementations map[string][]string // interface method id -> concrete method ids
	lastRefresh     time.Time
	refreshMu       sync.Mutex // For serializing Refresh calls
}

const refreshTTL = 5 * time.Second

func newIndexer(dir string) (*indexer, error) {
	return &indexer{
		dir:             dir,
		fset:            token.NewFileSet(),
		symbolsByPath:   make(map[string][]symbolLocation),
		usagesByName:    make(map[string][]location),
		implementations: make(map[string][]string),
	}, nil
}

func (idx *indexer) Refresh(ctx context.Context) error {
	if !idx.needsRefresh() {
		return nil
	}

	// Serialize concurrent refresh attempts
	idx.refreshMu.Lock()
	defer idx.refreshMu.Unlock()

	// Double check after acquiring lock
	if !idx.needsRefresh() {
		return nil
	}

	fset := token.NewFileSet()
	pkgs, err := idx.loadPackages(ctx, fset)
	if err != nil {
		return err
	}

	// Parallel AST harvesting
	type pkgResult struct {
		symbols map[string][]symbolLocation
		usages  map[string][]location
	}

	results := make(chan pkgResult, len(pkgs))
	var wg sync.WaitGroup

	for _, pkg := range pkgs {
		wg.Add(1)
		go func(p *packages.Package) {
			defer wg.Done()
			h := newHarvester(fset)
			h.info = p.TypesInfo

			for _, file := range p.Syntax {
				filename := fset.File(file.Pos()).Name()
				h.currentPath, _ = filepath.Abs(filename)
				ast.Inspect(file, h.visit)
			}

			results <- pkgResult{
				symbols: h.symbolsByPath,
				usages:  h.usagesByName,
			}
		}(pkg)
	}

	// Wait for all workers to finish and close the results channel
	go func() {
		wg.Wait()
		close(results)
	}()

	// Merge results (Reduce phase)
	symbolsByPath := make(map[string][]symbolLocation)
	usagesByName := make(map[string][]location)

	for res := range results {
		for path, symbols := range res.symbols {
			symbolsByPath[path] = symbols
		}
		for name, locations := range res.usages {
			usagesByName[name] = append(usagesByName[name], locations...)
		}
	}

	idx.updateState(pkgs, symbolsByPath, usagesByName, fset)
	return nil
}

func (idx *indexer) toLocation(pos token.Pos) location {
	p := idx.fset.Position(pos)
	abs, _ := filepath.Abs(p.Filename)
	return location{
		Path:   abs,
		Line:   p.Line,
		Column: p.Column,
	}
}

func (idx *indexer) Lookup(ctx context.Context, symbol string) ([]location, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var locations []location
	for _, pkg := range idx.pkgs {
		obj := pkg.Types.Scope().Lookup(symbol)
		if obj != nil {
			locations = append(locations, idx.toLocation(obj.Pos()))
		}
	}
	return locations, nil
}

func (idx *indexer) FindImplementors(ctx context.Context, interfaceName string) ([]typeName, error) {
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

	var implementors []typeName
	for _, pkg := range idx.pkgs {
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if _, ok := obj.Type().Underlying().(*types.Interface); ok {
				continue
			}

			if types.Implements(obj.Type(), iface) || types.Implements(types.NewPointer(obj.Type()), iface) {
				implementors = append(implementors, typeName{
					PkgPath: pkg.PkgPath,
					Name:    name,
				})
			}
		}
	}

	return implementors, nil
}

func (idx *indexer) SearchSymbols(ctx context.Context, path string, query string, exportedOnly bool) ([]symbolLocation, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	searchPath, err := filepath.Abs(path)
	if err != nil {
		searchPath = path
	}

	var results []symbolLocation
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

func (idx *indexer) GetUsages(ctx context.Context, symbol string, path string) ([]location, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	searchPath, err := filepath.Abs(path)
	if err != nil {
		searchPath = path
	}

	var results []location
	usages, ok := idx.usagesByName[symbol]
	if !ok {
		return nil, nil
	}

	for _, loc := range usages {
		// Optimized path check instead of filepath.Rel
		if loc.Path == searchPath {
			results = append(results, loc)
			continue
		}
		if strings.HasPrefix(loc.Path, searchPath) {
			if len(loc.Path) > len(searchPath) && loc.Path[len(searchPath)] == filepath.Separator {
				results = append(results, loc)
			}
		}
	}

	return results, nil
}

func (idx *indexer) Packages() []*packages.Package {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.pkgs
}

type harvester struct {
	fset          *token.FileSet
	symbolsByPath map[string][]symbolLocation
	usagesByName  map[string][]location
	currentPath   string
	info          *types.Info
}

func newHarvester(fset *token.FileSet) *harvester {
	return &harvester{
		fset:          fset,
		symbolsByPath: make(map[string][]symbolLocation),
		usagesByName:  make(map[string][]location),
	}
}

func (h *harvester) visit(n ast.Node) bool {
	if n == nil {
		return true
	}

	switch d := n.(type) {
	case *ast.GenDecl:
		h.handleGenDecl(d)
	case *ast.FuncDecl:
		h.handleFuncDecl(d)
	case *ast.Ident:
		h.handleIdent(d)
	}
	return true
}

func (h *harvester) handleGenDecl(d *ast.GenDecl) {
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.ValueSpec:
			h.handleValueSpec(d, s)
		case *ast.TypeSpec:
			h.handleTypeSpec(s)
		}
	}
}

func (h *harvester) handleValueSpec(d *ast.GenDecl, s *ast.ValueSpec) {
	kind := "var"
	if d.Tok == token.CONST {
		kind = "const"
	}
	for _, name := range s.Names {
		loc := h.toLocation(name.Pos())
		h.symbolsByPath[h.currentPath] = append(h.symbolsByPath[h.currentPath], symbolLocation{
			location: loc,
			Name:     name.Name,
			Kind:     kind,
		})
	}
}

func (h *harvester) handleTypeSpec(s *ast.TypeSpec) {
	loc := h.toLocation(s.Name.Pos())
	h.symbolsByPath[h.currentPath] = append(h.symbolsByPath[h.currentPath], symbolLocation{
		location: loc,
		Name:     s.Name.Name,
		Kind:     "type",
	})
}

func (h *harvester) handleFuncDecl(d *ast.FuncDecl) {
	kind := "func"
	sig := getFuncSignature(d)
	var recv string
	if d.Recv != nil && len(d.Recv.List) > 0 {
		recv = exprToString(d.Recv.List[0].Type)
	}
	loc := h.toLocation(d.Name.Pos())
	h.symbolsByPath[h.currentPath] = append(h.symbolsByPath[h.currentPath], symbolLocation{
		location:  loc,
		Name:      d.Name.Name,
		Kind:      kind,
		Signature: sig,
		Receiver:  recv,
	})
}

func (h *harvester) handleIdent(d *ast.Ident) {
	if h.info == nil {
		return
	}

	if _, isDef := h.info.Defs[d]; isDef {
		return
	}

	obj, ok := h.info.Uses[d]
	if !ok || obj == nil {
		return
	}

	// Filter: record only exported symbols, method calls, or package-level symbols.
	// This reduces noise from local variables and parameters (which comprise the bulk of identifiers)
	// while keeping relevant symbols for cross-package and package-level analysis.
	isExported := obj.Exported()
	isMethod := false
	if fn, ok := obj.(*types.Func); ok {
		sig := fn.Type().(*types.Signature)
		isMethod = sig.Recv() != nil
	}

	isPackageLevel := false
	if obj.Pkg() != nil && obj.Parent() == obj.Pkg().Scope() {
		isPackageLevel = true
	}

	if !isExported && !isMethod && !isPackageLevel {
		return
	}

	// Move toLocation after the filter to avoid unnecessary allocations
	loc := h.toLocation(d.Pos())

	if isExported {
		// Only call expensive getSymbolIdentity for exported symbols
		key := getSymbolIdentity(obj)
		h.usagesByName[key] = append(h.usagesByName[key], loc)
		if key != d.Name {
			h.usagesByName[d.Name] = append(h.usagesByName[d.Name], loc)
		}
	} else {
		// Private method call or package-level symbol: store by short name to save memory
		h.usagesByName[d.Name] = append(h.usagesByName[d.Name], loc)
	}
}

func (h *harvester) toLocation(pos token.Pos) location {
	p := h.fset.Position(pos)
	// Optimization: use h.currentPath which is already absolute
	return location{
		Path:   h.currentPath,
		Line:   p.Line,
		Column: p.Column,
	}
}

func (idx *indexer) needsRefresh() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return time.Since(idx.lastRefresh) > refreshTTL
}

func (idx *indexer) loadPackages(ctx context.Context, fset *token.FileSet) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedModule,
		Dir:     idx.dir,
		Fset:    fset,
		Context: ctx,
		Tests:   true,
	}
	return packages.Load(cfg, "./...")
}

func (idx *indexer) updateState(pkgs []*packages.Package, symbolsByPath map[string][]symbolLocation, usagesByName map[string][]location, fset *token.FileSet) {
	impls := idx.computeImplementations(pkgs)

	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.pkgs = pkgs
	idx.fset = fset
	idx.symbolsByPath = symbolsByPath
	idx.usagesByName = usagesByName
	idx.implementations = impls
	idx.lastRefresh = time.Now()
}

func (idx *indexer) collectInterfaces(pkgs []*packages.Package) []*types.Interface {
	var interfaces []*types.Interface
	for _, pkg := range pkgs {
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if tn, ok := obj.(*types.TypeName); ok {
				if itf, ok := tn.Type().Underlying().(*types.Interface); ok {
					interfaces = append(interfaces, itf)
				}
			}
		}
	}
	return interfaces
}

func (idx *indexer) asConcreteNamedType(obj types.Object) (*types.Named, bool) {
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, false
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		return nil, false
	}
	if _, ok := named.Underlying().(*types.Interface); ok {
		return nil, false
	}
	return named, true
}

func (idx *indexer) mapTypeToInterfaces(impls map[string][]string, named *types.Named, interfaces []*types.Interface, pkgTypes *types.Package) {
	for _, itf := range interfaces {
		if types.Implements(named, itf) || types.Implements(types.NewPointer(named), itf) {
			for i := 0; i < itf.NumMethods(); i++ {
				im := itf.Method(i)
				imId := getSymbolIdentity(im)

				cm, _, _ := types.LookupFieldOrMethod(named, true, pkgTypes, im.Name())
				if cm != nil {
					cmId := getSymbolIdentity(cm)
					impls[imId] = append(impls[imId], cmId)
				}
			}
		}
	}
}

func (idx *indexer) computeImplementations(pkgs []*packages.Package) map[string][]string {
	impls := make(map[string][]string)
	ifaces := idx.collectInterfaces(pkgs)

	for _, pkg := range pkgs {
		for _, obj := range pkg.TypesInfo.Defs {
			if named, ok := idx.asConcreteNamedType(obj); ok {
				idx.mapTypeToInterfaces(impls, named, ifaces, pkg.Types)
			}
		}
	}
	return impls
}

func (idx *indexer) GetImplementations(interfaceMethodId string) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.implementations[interfaceMethodId]
}

func (idx *indexer) IsSymbolUsed(name string) bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	usages, ok := idx.usagesByName[name]
	return ok && len(usages) > 0
}
