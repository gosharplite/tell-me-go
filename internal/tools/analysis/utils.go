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

// getSymbolIdentity creates a stable string representation for a Go symbol.
func getSymbolIdentity(obj types.Object) string {
	if obj == nil {
		return ""
	}
	if obj.Pkg() == nil {
		return obj.Name()
	}
	pkgPath := getBasePkgPath(obj.Pkg().Path())

	if fn, ok := obj.(*types.Func); ok {
		sig, ok := fn.Type().(*types.Signature)
		if !ok {
			return fmt.Sprintf("%s.%s", pkgPath, obj.Name())
		}
		if sig.Recv() != nil {
			recvType := sig.Recv().Type()
			if ptr, ok := recvType.(*types.Pointer); ok {
				recvType = ptr.Elem()
			}

			var typeName string
			if named, ok := recvType.(*types.Named); ok {
				typeName = named.Obj().Name()
			} else if tp, ok := recvType.(*types.TypeParam); ok {
				typeName = tp.Obj().Name()
			} else {
				// Handle other types if necessary, but named is most common for methods
				typeName = strings.TrimPrefix(recvType.String(), pkgPath+".")
			}

			// Strip generic type parameters like [T] from the identity for stability
			if idx := strings.Index(typeName, "["); idx != -1 {
				typeName = typeName[:idx]
			}

			return fmt.Sprintf("%s.%s.%s", pkgPath, typeName, obj.Name())
		}
	}
	return fmt.Sprintf("%s.%s", pkgPath, obj.Name())
}

func getBasePkgPath(path string) string {
	if idx := strings.Index(path, " ["); idx != -1 {
		return path[:idx]
	}
	return path
}
