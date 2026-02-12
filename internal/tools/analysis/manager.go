// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/exec"
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

// analysisManager is the consolidated hub for all code analysis, refactoring, and development tools.
type analysisManager struct {
	Complexity IComplexityAnalyzer
	Dependency IDependencyAnalyzer
	Sequence   ISequenceAnalyzer
	Change     IChangeAnalyzer
	Types      ITypeManager
	DeadCode   IDeadCodeAnalyzer

	// Refactoring
	Refactor *refactorManager

	// Information & Search
	Info   *infoManager
	Search *searchManager

	// Project Health & Architecture
	Health *healthManager
	Arch   *architectureManager

	// EventBus for progress reporting
	Events events.EventBus
}

func newAnalysisManager(idx symbolIndex, cache *astCache, sp security.SecurityProvider, bus events.EventBus) *analysisManager {
	executor := &exec.RealExecutor{}
	fs := storage.DefaultFileSystem

	m := &analysisManager{
		Complexity: newComplexityAnalyzer(cache, sp),
		Dependency: newDependencyAnalyzer(executor, sp, bus),
		Sequence:   newSequenceAnalyzer(executor, sp),
		Change:     newChangeAnalyzer(cache, executor),
		Types:      newTypeManager(idx, cache, sp),
		DeadCode:   newDeadCodeAnalyzer(sp),

		Refactor: newRefactorManager(sp),
		Info:     &infoManager{SP: sp, Cache: cache, FS: fs, Events: bus},
		Search:   &searchManager{SP: sp, FS: fs},
		Arch:     &architectureManager{SP: sp},
		Events:   bus,
	}

	m.Arch.Loader = &RealPackageProvider{m: m.Arch}
	m.Health = &healthManager{SP: sp, Ana: m}

	return m
}

// Delegated methods for registration

func (m *analysisManager) FindOrphanedSymbols(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.DeadCode.FindOrphanedSymbols(ctx, args)
}

func (m *analysisManager) AnalyzeComplexity(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Complexity.Analyze(ctx, args)
}

func (m *analysisManager) GetPackageGraph(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Dependency.GetPackageGraph(ctx, args)
}

func (m *analysisManager) AnalyzeSequenceFlow(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Sequence.AnalyzeSequenceFlow(ctx, args)
}

func (m *analysisManager) SemanticDiff(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Change.SemanticDiff(ctx, args)
}

func (m *analysisManager) ListImplementations(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Types.ListImplementations(ctx, args)
}

func (m *analysisManager) FindUsages(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Types.FindUsages(ctx, args)
}

func (m *analysisManager) ListSymbols(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Types.ListSymbols(ctx, args)
}

func (m *analysisManager) GetTypeInfo(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Types.GetTypeInfo(ctx, args)
}

func (m *analysisManager) FindDefinitions(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	return m.Types.FindDefinitions(ctx, args)
}
