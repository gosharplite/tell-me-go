// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build arch

package analysis

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestVerifyPortsRegistry is the live gate for the ports registry (issue
// #1343, ADR-064): it recomputes the real ports-package declarations against
// the LIVE indexer and cross-references them against the live
// internal/domain/ports/doc.go `// # Registry` block, then enforces the
// structural checks: the registry bijection, the N ≤ 12 family bound, the
// ADR-056 stay-key liveness, and the 5-clause Supporting admission rule.
// Zero violations means every ports export is admitted and the roster is
// exactly the architect's curated membership.
func TestVerifyPortsRegistry(t *testing.T) {
	idx := getRealArchitectureIndexer(t)
	analyzer := &defaultDeadCodeAnalyzer{idx: idx}

	pkgs, err := idx.Packages(context.Background(), nil)
	require.NoError(t, err)

	modulePath := detectModulePath(pkgs)
	require.NotEmpty(t, modulePath, "failed to detect module path for ports registry gate")

	root, err := findModuleRoot()
	require.NoError(t, err)

	state := &scanState{pkgs: pkgs, targetModule: modulePath, targetPath: root}

	// Enumerate ports declarations, filtering to the ports package and to the
	// Type/Function/Constant/Variable symbol kinds, deduplicating by name
	// (the indexer loads Tests:true, so the in-package test variant
	// re-harvests the same production symbols).
	seen := make(map[string]bool)
	var decls []*symMeta
	err = idx.HarvestDeclarations(context.Background(), func(meta *symMeta) bool {
		if meta == nil || !isPortsPackagePath(meta.pkgPath) {
			return true
		}
		switch meta.symType {
		case "Type", "Function", "Constant", "Variable":
		default:
			return true // drop Method/Unknown
		}
		if seen[meta.name] {
			return true
		}
		seen[meta.name] = true
		decls = append(decls, meta)
		return true
	}, nil)
	require.NoError(t, err)

	reg, err := loadPortsRegistry()
	require.NoError(t, err)

	var violations []string
	violations = append(violations, verifyPortsRegistryBijection(reg, decls)...)
	violations = append(violations, verifyPortsRegistryFamilyBound(reg)...)
	violations = append(violations, verifyPortsRegistryStayKeyLiveness(decls)...)
	violations = append(violations, analyzer.verifyPortsSupportingAdmission(reg, decls, state, nil)...)

	if len(violations) > 0 {
		t.Errorf("ports registry verification FAILED (%d violations):\n%s", len(violations), strings.Join(violations, "\n"))
	}
}
