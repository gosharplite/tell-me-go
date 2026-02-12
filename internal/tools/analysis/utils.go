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
		sig := fn.Type().(*types.Signature)
		if sig.Recv() != nil {
			recvType := sig.Recv().Type()
			if ptr, ok := recvType.(*types.Pointer); ok {
				recvType = ptr.Elem()
			}
			if named, ok := recvType.(*types.Named); ok {
				return fmt.Sprintf("%s.%s.%s", pkgPath, named.Obj().Name(), obj.Name())
			}
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
