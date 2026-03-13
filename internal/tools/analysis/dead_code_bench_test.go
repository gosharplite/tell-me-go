// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"go/types"
	"path/filepath"
	"testing"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

type benchmarkSecurityManager struct {
	domain_security.ISecurityManager
}

func (m *benchmarkSecurityManager) IsPathSafe(path string) (string, error) {
	return filepath.Abs(path)
}

func (m *benchmarkSecurityManager) IsPathWritable(path string) (string, error) {
	return m.IsPathSafe(path)
}

func (m *benchmarkSecurityManager) TerminalLock()   {}
func (m *benchmarkSecurityManager) TerminalUnlock() {}
func (m *benchmarkSecurityManager) IsBypassActive() bool {
	return false
}
func (m *benchmarkSecurityManager) IsCommandAllowed(command string) bool {
	return true
}
func (m *benchmarkSecurityManager) Prompt(message string) {}
func (m *benchmarkSecurityManager) Warn(message string)   {}
func (m *benchmarkSecurityManager) Confirm(ctx context.Context, message string) (bool, error) {
	return true, nil
}
func (m *benchmarkSecurityManager) LogAudit(label1, val1, label2, val2 string) {}
func (m *benchmarkSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	return true, nil
}

func BenchmarkDeadCode_ColdScan(b *testing.B) {
	ctx := context.Background()
	args := map[string]interface{}{"path": "."}
	sm := &benchmarkSecurityManager{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx, err := newIndexer(".")
		if err != nil {
			b.Fatal(err)
		}
		analyzer := newDeadCodeAnalyzer(sm, idx)
		_, err = analyzer.FindOrphanedSymbols(ctx, args)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDeadCode_CachedScan(b *testing.B) {
	ctx := context.Background()
	args := map[string]interface{}{"path": "."}
	sm := &benchmarkSecurityManager{}

	idx, err := newIndexer(".")
	if err != nil {
		b.Fatal(err)
	}
	analyzer := newDeadCodeAnalyzer(sm, idx)

	// Warm up cache
	_, err = analyzer.FindOrphanedSymbols(ctx, args)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = analyzer.FindOrphanedSymbols(ctx, args)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCalculateImpactScore_ProjectWide(b *testing.B) {
	ctx := context.Background()
	idx, _ := newIndexer(".")
	sm := &benchmarkSecurityManager{}
	analyzer := newDeadCodeAnalyzer(sm, idx)
	pkgs, _ := idx.Packages(ctx)

	// Collect all function objects in the project
	var funcs []types.Object
	for _, pkg := range pkgs {
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if _, ok := obj.(*types.Func); ok {
				funcs = append(funcs, obj)
			}
		}
	}

	if len(funcs) == 0 {
		b.Fatal("no functions found to benchmark")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Benchmark analysis of the first 20 functions to simulate a typical report
		for j := 0; j < 20 && j < len(funcs); j++ {
			_ = analyzer.calculateImpactScore(funcs[j], pkgs)
		}
	}
}
