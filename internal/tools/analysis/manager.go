// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

// Analyzer interfaces for segregation and testing
type IComplexityAnalyzer interface {
	Analyze(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error)
	GatherComplexities(ctx context.Context, root string) ([]FuncComplexity, error)
}

type IDependencyAnalyzer interface {
	GetPackageGraph(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error)
}

type ISequenceAnalyzer interface {
	AnalyzeSequenceFlow(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error)
}

type IChangeAnalyzer interface {
	SemanticDiff(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error)
}

type ITypeManager interface {
	GetTypeInfo(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error)
	ListSymbols(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error)
	ListImplementations(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error)
	FindUsages(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error)
	FindDefinitions(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error)
}

type IDeadCodeAnalyzer interface {
	FindOrphanedSymbols(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error)
}

// AnalysisManager is the consolidated hub for all code analysis, refactoring, and development tools.
type AnalysisManager struct {
	Complexity IComplexityAnalyzer
	Dependency IDependencyAnalyzer
	Sequence   ISequenceAnalyzer
	Change     IChangeAnalyzer
	Types      ITypeManager
	DeadCode   IDeadCodeAnalyzer

	// Refactoring
	Refactor *RefactorManager

	// Information & Search
	Info   *InfoManager
	Search *SearchManager

	// Project Health & Architecture
	Health *HealthManager
	Arch   *ArchitectureManager
}

func NewAnalysisManager(idx SymbolIndex, cache *ASTCache, sp security.SecurityProvider) *AnalysisManager {
	exec := &RealExecutor{}
	fs := storage.DefaultFileSystem

	m := &AnalysisManager{
		Complexity: NewComplexityAnalyzer(cache, sp),
		Dependency: NewDependencyAnalyzer(exec, sp),
		Sequence:   NewSequenceAnalyzer(exec, sp),
		Change:     NewChangeAnalyzer(cache, exec),
		Types:      NewTypeManager(idx, cache, sp),
		DeadCode:   NewDeadCodeAnalyzer(sp),

		Refactor: NewRefactorManager(sp),
		Info:     &InfoManager{SP: sp, Cache: cache, FS: fs},
		Search:   &SearchManager{SP: sp, FS: fs},
		Arch:     &ArchitectureManager{SP: sp},
	}

	m.Arch.Loader = &RealPackageProvider{m: m.Arch}
	m.Health = &HealthManager{SP: sp, Ana: m}

	return m
}

// Delegated methods for registration

func (m *AnalysisManager) FindOrphanedSymbols(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.DeadCode.FindOrphanedSymbols(ctx, args)
}

func (m *AnalysisManager) AnalyzeComplexity(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Complexity.Analyze(ctx, args)
}

func (m *AnalysisManager) GetPackageGraph(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Dependency.GetPackageGraph(ctx, args)
}

func (m *AnalysisManager) AnalyzeSequenceFlow(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Sequence.AnalyzeSequenceFlow(ctx, args)
}

func (m *AnalysisManager) SemanticDiff(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Change.SemanticDiff(ctx, args)
}

func (m *AnalysisManager) ListImplementations(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Types.ListImplementations(ctx, args)
}

func (m *AnalysisManager) FindUsages(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Types.FindUsages(ctx, args)
}

func (m *AnalysisManager) ListSymbols(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Types.ListSymbols(ctx, args)
}

func (m *AnalysisManager) GetTypeInfo(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Types.GetTypeInfo(ctx, args)
}

func (m *AnalysisManager) FindDefinitions(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Types.FindDefinitions(ctx, args)
}
