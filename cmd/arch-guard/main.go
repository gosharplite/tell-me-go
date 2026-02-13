package main

import (
	"fmt"
	"go/types"
	"log"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

func main() {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
		Tests: true,
	}

	fmt.Println("Loading packages...")
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		log.Fatal(err)
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
				categorize(obj, pkg, usages, ifaceList, processedObjects)
			}

			// Exported methods of any type (even private types)
			if tn, ok := obj.(*types.TypeName); ok {
				if named, ok := tn.Type().(*types.Named); ok {
					for i := 0; i < named.NumMethods(); i++ {
						method := named.Method(i)
						if method.Exported() {
							categorize(method, pkg, usages, ifaceList, processedObjects)
						}
					}
				}
			}
		}
	}
}

func categorize(obj types.Object, pkg *packages.Package, usages map[types.Object]map[string]bool, allInterfaces []*types.TypeName, processed map[string]bool) {
	fullName := ""
	if f, ok := obj.(*types.Func); ok && f.Type().(*types.Signature).Recv() != nil {
		fullName = f.FullName()
	} else {
		fullName = fmt.Sprintf("%s.%s", pkg.PkgPath, obj.Name())
	}

	if processed[fullName] {
		return
	}
	processed[fullName] = true

	pos := pkg.Fset.Position(obj.Pos())
	fileName := filepath.Base(pos.Filename)

	// Filter 2: Go Toolchain Hooks
	if isToolchainHook(obj.Name(), fileName) {
		fmt.Printf("[ARCHITECTURAL BOUNDARY] %s (Toolchain Hook)\n", fullName)
		return
	}

	// Filter 1: Interface Satisfaction (for methods)
	if f, ok := obj.(*types.Func); ok && f.Type().(*types.Signature).Recv() != nil {
		if satisfiesExternalInterface(f, allInterfaces) {
			fmt.Printf("[ARCHITECTURAL BOUNDARY] %s (Interface Satisfaction)\n", fullName)
			return
		}
	}

	// Usage check
	if isUsedOutside(obj, pkg.PkgPath, usages) {
		fmt.Printf("[ARCHITECTURAL BOUNDARY] %s (Used outside package)\n", fullName)
		return
	}

	fmt.Printf("[PRIVATE CANDIDATE] %s\n", fullName)
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
