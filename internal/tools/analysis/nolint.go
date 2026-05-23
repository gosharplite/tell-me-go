// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"go/token"
	"go/types"
	"os"
	"strings"
)

// findFileForPosition returns the *token.File that contains pos by
// searching all filesets in state.pkgs. Returns nil if no fileset
// contains pos or pos is invalid.
func findFileForPosition(pos token.Pos, state *scanState) *token.File {
	if !pos.IsValid() {
		return nil
	}
	for _, pkg := range state.pkgs {
		if pkg.Fset != nil {
			if f := pkg.Fset.File(pos); f != nil {
				return f
			}
		}
	}
	return nil
}

// hasNolintDirective reports whether the directive "nolint:deadcode"
// appears on line declLine or the line immediately before it in the
// given source lines (1-indexed).
func hasNolintDirective(lines []string, declLine int) bool {
	if declLine > 0 && declLine <= len(lines) {
		if strings.Contains(lines[declLine-1], "nolint:deadcode") {
			return true
		}
	}
	prevLine := declLine - 1
	if prevLine > 0 && prevLine <= len(lines) {
		if strings.Contains(lines[prevLine-1], "nolint:deadcode") {
			return true
		}
	}
	return false
}

// isNolintDeadcode reports whether obj's declaration is annotated with
// a //nolint:deadcode directive. It checks the line immediately preceding
// the declaration and any comment group on the same line.
func isNolintDeadcode(obj types.Object, state *scanState) bool {
	pos := obj.Pos()
	f := findFileForPosition(pos, state)
	if f == nil {
		return false
	}

	src, err := os.ReadFile(f.Name())
	if err != nil {
		return false
	}

	lines := strings.Split(string(src), "\n")
	return hasNolintDirective(lines, f.Line(pos))
}
