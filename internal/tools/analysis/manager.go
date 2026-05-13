// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/toolchain"
)

// Analyzer interfaces for segregation and testing
type complexityAnalyzer interface {
	Analyze(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error)
	GatherComplexities(ctx context.Context, root string, hb chan<- struct{}) ([]funcComplexity, error)
}

type dependencyAnalyzer interface {
	GetPackageGraph(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error)
}

type sequenceAnalyzer interface {
	AnalyzeSequenceFlow(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error)
}

type changeAnalyzer interface {
	SemanticDiff(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error)
}

type typeManager interface {
	GetTypeInfo(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error)
	ListSymbols(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error)
	ListImplementations(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error)
	FindUsages(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error)
	FindDefinitions(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error)
}

type deadCodeAnalyzer interface {
	FindOrphanedSymbols(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error)
	GatherOrphanReports(ctx context.Context, path string, hb chan<- struct{}) ([]orphanReport, error)
}

// AnalysisGoRunner defines the required Go toolchain methods for analysis.
type AnalysisGoRunner interface {
	GetPackageList(ctx context.Context, path string) ([]byte, error)
	GetGoDoc(ctx context.Context, symbol string) ([]byte, error)
	GetModulePath(ctx context.Context) (string, error)
	GetModuleDir(ctx context.Context) (string, error)
	RunTestsWithCoverage(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error)
	RunLinter(ctx context.Context) (string, string, error)
}

// analysisManager is the consolidated hub for all code analysis, refactoring, and development tools.
type analysisManager struct {
	Complexity complexityAnalyzer
	Dependency dependencyAnalyzer
	Sequence   sequenceAnalyzer
	Change     changeAnalyzer
	Types      typeManager
	deadCode   deadCodeAnalyzer

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

func newAnalysisManager(idx symbolIndex, cache *astCache, sp domain_security.Manager, bus events.EventBus, executor tools.CommandExecutor, fs persistence.FileSystem, wp services.WorkspacePolicy, dc deadCodeAnalyzer) *analysisManager {
	runner := toolchain.NewGoRunner(executor)
	m := &analysisManager{
		Complexity: newComplexityAnalyzer(cache, sp),
		Dependency: newDependencyAnalyzer(runner, sp, bus, wp),
		Sequence:   newSequenceAnalyzer(executor, sp, idx),
		Change:     newChangeAnalyzer(cache, executor),
		Types:      newTypeManager(idx, cache, sp),
		deadCode:   dc,

		Refactor: newRefactorManager(sp),
		Info:     &infoManager{SP: sp, Cache: cache, FS: fs, Events: bus, Runner: runner, Policy: wp},
		Search:   &searchManager{SP: sp, FS: fs, Policy: wp},
		Arch:     &architectureManager{SP: sp, Runner: runner, idx: idx},
		Events:   bus,
	}

	m.Health = &healthManager{
		SP:     sp,
		Ana:    m,
		Exec:   executor,
		Runner: runner,
	}

	return m
}

// Delegated methods for registration

func (m *analysisManager) FindOrphanedSymbols(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.deadCode.FindOrphanedSymbols(ctx, args, hb)
}

func (m *analysisManager) AnalyzeComplexity(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.Complexity.Analyze(ctx, args, hb)
}

func (m *analysisManager) GetPackageGraph(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.Dependency.GetPackageGraph(ctx, args, hb)
}

func (m *analysisManager) AnalyzeSequenceFlow(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.Sequence.AnalyzeSequenceFlow(ctx, args, hb)
}

func (m *analysisManager) SemanticDiff(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.Change.SemanticDiff(ctx, args, hb)
}

func (m *analysisManager) ListImplementations(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.Types.ListImplementations(ctx, args, hb)
}

func (m *analysisManager) FindUsages(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.Types.FindUsages(ctx, args, hb)
}

func (m *analysisManager) ListSymbols(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.Types.ListSymbols(ctx, args, hb)
}

func (m *analysisManager) GetTypeInfo(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.Types.GetTypeInfo(ctx, args, hb)
}

func (m *analysisManager) FindDefinitions(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.Types.FindDefinitions(ctx, args, hb)
}
