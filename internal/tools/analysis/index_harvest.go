// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
	"golang.org/x/tools/go/packages"
)

// pkgResult holds the results of harvesting symbols and usages from a package.
type pkgResult struct {
	symbols map[string][]symbolLocation
	usages  map[string][]location
}

// sendResult sends a pkgResult on the results channel, emitting a heartbeat on
// success. It respects context cancellation.
func sendResult(ctx context.Context, results chan<- pkgResult, res pkgResult, hb chan<- struct{}) error {
	select {
	case results <- res:
		if hb != nil {
			select {
			case hb <- struct{}{}:
			default:
			}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (idx *indexer) harvestPackages(ctx context.Context, fset *token.FileSet, pkgs []*packages.Package, hb chan<- struct{}) (map[string][]symbolLocation, map[string][]location, error) {
	results := make(chan pkgResult, len(pkgs))
	g, gCtx := errgroup.WithContext(ctx)

	limit := int64(runtime.NumCPU())
	if limit < 1 {
		limit = 1
	}
	sem := semaphore.NewWeighted(limit)

	for _, pkg := range pkgs {
		p := pkg // Captured for closure
		g.Go(func() error {
			if err := sem.Acquire(gCtx, 1); err != nil {
				return err
			}
			defer sem.Release(1)

			res, err := idx.processPackage(gCtx, fset, p)
			if err != nil {
				return err
			}
			return sendResult(gCtx, results, res, hb)
		})
	}

	// Wait for all workers to finish and close the results channel
	go func() {
		_ = g.Wait()
		close(results)
	}()

	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	return idx.mergeResults(results)
}

func (idx *indexer) processPackage(ctx context.Context, fset *token.FileSet, pkg *packages.Package) (pkgResult, error) {
	h := newHarvester(fset)
	h.info = pkg.TypesInfo

	for _, file := range pkg.Syntax {
		if ctx.Err() != nil {
			return pkgResult{}, ctx.Err()
		}
		if err := idx.processFile(fset, file, h); err != nil {
			return pkgResult{}, err
		}
	}

	return pkgResult{
		symbols: h.symbolsByPath,
		usages:  h.usagesByName,
	}, nil
}

func (idx *indexer) processFile(fset *token.FileSet, file *ast.File, h *harvester) error {
	filename := fset.File(file.Pos()).Name()
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return err
	}

	// Although the file is already parsed into memory, we may want to skip it based on metadata
	// or perform validation.
	info, err := os.Stat(absPath)
	if err != nil {
		// In some environments, files might be deleted between Load and Refresh
		return nil
	}

	if !idx.shouldIndexFile(absPath, info) {
		return nil
	}

	h.currentPath = absPath
	ast.Inspect(file, h.visit)
	return nil
}

func (idx *indexer) shouldIndexFile(path string, info os.FileInfo) bool {
	if info.IsDir() {
		return false
	}
	return strings.HasSuffix(path, ".go")
}

func (idx *indexer) mergeResults(results <-chan pkgResult) (map[string][]symbolLocation, map[string][]location, error) {
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
	return symbolsByPath, usagesByName, nil
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

	obj, ok := h.info.Uses[d]
	if !ok || obj == nil {
		return // Not a usage of any symbol (e.g., pure definition with no simultaneous use)
	}

	// NOTE: An ident can be both a Def AND a Use simultaneously — the
	// canonical example is a struct embed like `analysistest.MockSymbolIndex`
	// where the Sel ident defines the embedded field while also referencing
	// the type from another package. The previous code unconditionally
	// returned on any Def match, which silently dropped all struct-embed
	// usages. We now only gate on the Uses map; a pure Def (e.g., a func
	// declaration name) has Uses[d] == nil and is correctly skipped above.

	isExported, isMethod, isPackageLevel := h.classifyObject(obj)
	if !isExported && !isMethod && !isPackageLevel {
		return
	}

	// Move toLocation after the filter to avoid unnecessary allocations
	loc := h.toLocation(d.Pos())
	h.recordUsage(d.Name, obj, loc, isExported)
}

func (h *harvester) classifyObject(obj types.Object) (isExported, isMethod, isPackageLevel bool) {
	isExported = obj.Exported()

	if fn, ok := obj.(*types.Func); ok {
		sig := fn.Type().(*types.Signature)
		isMethod = sig.Recv() != nil
	}

	if obj.Pkg() != nil && obj.Parent() == obj.Pkg().Scope() {
		isPackageLevel = true
	}

	return
}

func (h *harvester) recordUsage(shortName string, obj types.Object, loc location, isExported bool) {
	if isExported {
		// Only call expensive getSymbolIdentity for exported symbols
		key := getSymbolIdentity(obj)
		h.usagesByName[key] = append(h.usagesByName[key], loc)
		if key != shortName {
			h.usagesByName[shortName] = append(h.usagesByName[shortName], loc)
		}
	} else {
		// Private method call or package-level symbol: store by short name to save memory
		h.usagesByName[shortName] = append(h.usagesByName[shortName], loc)
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
