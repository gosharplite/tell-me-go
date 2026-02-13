package archguard

import (
	"context"
	"fmt"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

type Category string

const (
	ArchitecturalBoundary Category = "ARCHITECTURAL BOUNDARY"
	PrivateCandidate      Category = "PRIVATE CANDIDATE"
)

type Finding struct {
	Symbol   string
	Category Category
	Reason   string
}

func Analyze(ctx context.Context, rootPath string) ([]Finding, error) {
	cfg := &packages.Config{
		Context: ctx,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
		Tests: true,
	}

	pkgs, err := packages.Load(cfg, rootPath)
	if err != nil {
		return nil, err
	}

	// 1. Collect all interfaces defined in the project and its dependencies
	allInterfaces := make(map[string]*types.TypeName)
	visited := make(map[string]bool)
	var collectInterfaces func(p *packages.Package)
	collectInterfaces = func(p *packages.Package) {
		if p == nil || visited[p.PkgPath] {
			return
		}
		visited[p.PkgPath] = true
		if p.Types != nil {
			scope := p.Types.Scope()
			for _, name := range scope.Names() {
				obj := scope.Lookup(name)
				if tn, ok := obj.(*types.TypeName); ok {
					if _, ok := tn.Type().Underlying().(*types.Interface); ok {
						allInterfaces[p.PkgPath+"."+name] = tn
					}
				}
			}
		}
		for _, imp := range p.Imports {
			collectInterfaces(imp)
		}
	}
	for _, pkg := range pkgs {
		collectInterfaces(pkg)
	}

	var ifaceList []*types.TypeName
	for _, itn := range allInterfaces {
		ifaceList = append(ifaceList, itn)
	}

	// 2. Track usages of all objects
	usages := make(map[types.Object]map[string]bool) // Object -> set of PkgPaths
	for _, pkg := range pkgs {
		for _, obj := range pkg.TypesInfo.Uses {
			if obj == nil || obj.Pkg() == nil {
				continue
			}
			if usages[obj] == nil {
				usages[obj] = make(map[string]bool)
			}
			usages[obj][pkg.PkgPath] = true
		}
		for _, sel := range pkg.TypesInfo.Selections {
			obj := sel.Obj()
			if obj == nil || obj.Pkg() == nil {
				continue
			}
			if usages[obj] == nil {
				usages[obj] = make(map[string]bool)
			}
			usages[obj][pkg.PkgPath] = true
		}
	}

	var findings []Finding
	processedObjects := make(map[string]bool)

	// 3. Identify symbols and categorize
	for _, pkg := range pkgs {
		if !strings.Contains(pkg.PkgPath, "/internal") {
			continue
		}
		if pkg.Types == nil {
			continue
		}

		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)

			// Top-level exported symbols
			if obj.Exported() {
				if f, ok := categorize(obj, pkg, usages, ifaceList, processedObjects); ok {
					findings = append(findings, f)
				}
			}

			// Exported methods of any type (even private types)
			if tn, ok := obj.(*types.TypeName); ok {
				if named, ok := tn.Type().(*types.Named); ok {
					for i := 0; i < named.NumMethods(); i++ {
						method := named.Method(i)
						if method.Exported() {
							if f, ok := categorize(method, pkg, usages, ifaceList, processedObjects); ok {
								findings = append(findings, f)
							}
						}
					}
				}
			}
		}
	}

	return findings, nil
}

func categorize(obj types.Object, pkg *packages.Package, usages map[types.Object]map[string]bool, allInterfaces []*types.TypeName, processed map[string]bool) (Finding, bool) {
	fullName := ""
	if f, ok := obj.(*types.Func); ok && f.Type().(*types.Signature).Recv() != nil {
		fullName = f.FullName()
	} else {
		fullName = fmt.Sprintf("%s.%s", pkg.PkgPath, obj.Name())
	}

	if processed[fullName] {
		return Finding{}, false
	}
	processed[fullName] = true

	pos := pkg.Fset.Position(obj.Pos())
	fileName := filepath.Base(pos.Filename)

	// Filter 2: Go Toolchain Hooks
	if isToolchainHook(obj.Name(), fileName) {
		return Finding{
			Symbol:   fullName,
			Category: ArchitecturalBoundary,
			Reason:   "Toolchain Hook",
		}, true
	}

	// Filter 1: Interface Satisfaction (for methods)
	if f, ok := obj.(*types.Func); ok && f.Type().(*types.Signature).Recv() != nil {
		if satisfiesExternalInterface(f, allInterfaces) {
			return Finding{
				Symbol:   fullName,
				Category: ArchitecturalBoundary,
				Reason:   "Interface Satisfaction",
			}, true
		}
	}

	// Usage check
	if isUsedOutside(obj, pkg.PkgPath, usages) {
		return Finding{
			Symbol:   fullName,
			Category: ArchitecturalBoundary,
			Reason:   "Used outside package",
		}, true
	}

	return Finding{
		Symbol:   fullName,
		Category: PrivateCandidate,
	}, true
}

func isToolchainHook(name string, fileName string) bool {
	if !strings.HasSuffix(fileName, "_test.go") {
		return false
	}
	prefixes := []string{"Test", "Benchmark", "Fuzz", "Example"}
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func isUsedOutside(obj types.Object, pkgPath string, usages map[types.Object]map[string]bool) bool {
	pkgUsages := usages[obj]
	for p := range pkgUsages {
		if p != pkgPath && p != pkgPath+"_test" {
			return true
		}
	}
	return false
}

func satisfiesExternalInterface(method *types.Func, allInterfaces []*types.TypeName) bool {
	sig := method.Type().(*types.Signature)
	recv := sig.Recv()
	if recv == nil {
		return false
	}

	recvType := recv.Type()
	typesToCheck := []types.Type{recvType}
	if _, ok := recvType.(*types.Pointer); !ok {
		typesToCheck = append(typesToCheck, types.NewPointer(recvType))
	}

	for _, itn := range allInterfaces {
		if itn.Pkg() == method.Pkg() {
			continue // Same package interface
		}
		iface, ok := itn.Type().Underlying().(*types.Interface)
		if !ok {
			continue
		}

		// Check if the method is part of this interface
		found := false
		for i := 0; i < iface.NumMethods(); i++ {
			if iface.Method(i).Name() == method.Name() {
				found = true
				break
			}
		}
		if !found {
			continue
		}

		for _, t := range typesToCheck {
			if types.Implements(t, iface) {
				return true
			}
		}
	}
	return false
}
