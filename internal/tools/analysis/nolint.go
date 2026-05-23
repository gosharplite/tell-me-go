// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"go/token"
	"go/types"
	"os"
	"strings"
)

// isNolintDeadcode reports whether obj's declaration is annotated with
// a //nolint:deadcode directive. It checks the line immediately preceding
// the declaration and any comment group on the same line.
func isNolintDeadcode(obj types.Object, state *scanState) bool {
	pos := obj.Pos()
	if !pos.IsValid() {
		return false
	}

	// Find the fileset that contains this position. Different packages
	// (including _test.go variants) may use different filesets, so we
	// must iterate all state.pkgs entries.
	var f *token.File
	for _, pkg := range state.pkgs {
		if pkg.Fset != nil {
			if f = pkg.Fset.File(pos); f != nil {
				break
			}
		}
	}
	if f == nil {
		return false
	}

	src, err := os.ReadFile(f.Name())
	if err != nil {
		return false
	}

	lines := strings.Split(string(src), "\n")
	declLine := f.Line(pos) // 1-indexed

	// Check the line containing the declaration for a block comment
	// (e.g., `type Foo struct{} /* nolint:deadcode */`).
	if declLine > 0 && declLine <= len(lines) {
		if strings.Contains(lines[declLine-1], "nolint:deadcode") {
			return true
		}
	}

	// Check the line immediately before the declaration for a
	// single-line comment (e.g., `//nolint:deadcode`).
	prevLine := declLine - 1
	if prevLine > 0 && prevLine <= len(lines) {
		if strings.Contains(lines[prevLine-1], "nolint:deadcode") {
			return true
		}
	}

	return false
}
