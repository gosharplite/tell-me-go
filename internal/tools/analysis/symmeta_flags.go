// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"go/types"
)

// isInterfaceTypeObj reports whether obj is a named interface type.
// Extracted from (*defaultDeadCodeAnalyzer).isInterfaceType for use
// by the indexer (which does not have access to the analyzer).
func isInterfaceTypeObj(obj types.Object) bool {
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return false
	}
	_, ok = tn.Type().Underlying().(*types.Interface)
	return ok
}

// isInterfaceMethodObj reports whether obj is a method on an interface type.
// Extracted from (*defaultDeadCodeAnalyzer).isInterfaceMethod for use
// by the indexer (which does not have access to the analyzer).
func isInterfaceMethodObj(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	// Defense-in-depth: *types.Func.Type() always returns *types.Signature
	// through the public go/types API, but the ok check guards against
	// unexpected future changes or edge cases in go/packages.
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	if sig.Recv() == nil {
		// Interface methods defined directly on an interface have nil
		// receivers in go/types.
		return true
	}
	_, ok = sig.Recv().Type().Underlying().(*types.Interface)
	return ok
}

// isWellKnownContractObj reports whether obj is a well-known stdlib
// contract method (e.g., io.Reader.Read, json.Marshaler.MarshalJSON).
// Extracted from (*defaultDeadCodeAnalyzer).isWellKnownContract for use
// by the indexer (which does not have access to the analyzer).
func isWellKnownContractObj(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	shapes, ok := wellKnownContractMethods[fn.Name()]
	if !ok {
		return false
	}
	for _, shape := range shapes {
		if signatureMatches(sig, shape) {
			return true
		}
	}
	return false
}
