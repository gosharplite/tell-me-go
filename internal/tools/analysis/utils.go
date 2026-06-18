// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"fmt"
	"go/types"
	"strings"
)

func sanitize(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, name)
}

// isMethod reports whether obj is a *types.Func with a signature that has a receiver.
func isMethod(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	return sig.Recv() != nil
}

// derefPointer unwraps a *types.Pointer to its element type.
// If t is not a pointer, it returns t unchanged.
// If t is nil, it returns nil.
func derefPointer(t types.Type) types.Type {
	if ptr, ok := t.(*types.Pointer); ok {
		return ptr.Elem()
	}
	return t
}

// extractTypeName resolves a receiver type to a human-readable name.
// It handles *types.Named, *types.TypeParam, and a fallback that
// strips the package path prefix from the type's string representation.
func extractTypeName(recvType types.Type, pkgPath string) string {
	if named, ok := recvType.(*types.Named); ok {
		return named.Obj().Name()
	}
	if tp, ok := recvType.(*types.TypeParam); ok {
		return tp.Obj().Name()
	}
	// Handle other types if necessary, but named is most common for methods
	return strings.TrimPrefix(recvType.String(), pkgPath+".")
}

// stripGenerics removes generic type parameters like [T] from a type name.
// For example, "MyType[T]" becomes "MyType".
// If no bracket is found, the name is returned unchanged.
func stripGenerics(name string) string {
	if idx := strings.Index(name, "["); idx != -1 {
		return name[:idx]
	}
	return name
}

// formatMethodIdentity builds the canonical identity string for a method:
//
//	pkgPath.TypeName.MethodName
//
// It dereferences the receiver, extracts the type name, and strips
// generic parameters for stability. If fn.Type() is not a *types.Signature,
// it falls back to pkgPath.FuncName (defensive — should not happen when
// called from getSymbolIdentity after isMethod returns true).
func formatMethodIdentity(pkgPath string, fn *types.Func) string {
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return fmt.Sprintf("%s.%s", pkgPath, fn.Name())
	}
	recvType := sig.Recv().Type()
	recvType = derefPointer(recvType)
	typeName := extractTypeName(recvType, pkgPath)
	typeName = stripGenerics(typeName)
	return fmt.Sprintf("%s.%s.%s", pkgPath, typeName, fn.Name())
}

// getSymbolIdentity creates a stable string representation for a Go symbol.
func getSymbolIdentity(obj types.Object) string {
	if obj == nil {
		return ""
	}
	if obj.Pkg() == nil {
		return obj.Name()
	}
	pkgPath := getBasePkgPath(obj.Pkg().Path())
	if isMethod(obj) {
		return formatMethodIdentity(pkgPath, obj.(*types.Func))
	}
	return fmt.Sprintf("%s.%s", pkgPath, obj.Name())
}

func getBasePkgPath(path string) string {
	if idx := strings.Index(path, " ["); idx != -1 {
		return path[:idx]
	}
	return path
}
