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
	GatherComplexities(ctx context.Context, root string, hb chan<- struct{}) ([]funcComplexity, []string, error)
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
	GatherOrphanReports(ctx context.Context, path string, deep bool, hb chan<- struct{}) ([]OrphanReport, error)
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
	complexity complexityAnalyzer
	dependency dependencyAnalyzer
	sequence   sequenceAnalyzer
	change     changeAnalyzer
	types      typeManager
	deadCode   deadCodeAnalyzer

	// Refactoring
	refactor *refactorManager

	// Information & Search
	info   *infoManager
	search *searchManager

	// Project Health & Architecture
	health *healthManager
	arch   *architectureManager
}

func newAnalysisManager(idx symbolIndex, cache *astCache, sp domain_security.Manager, bus events.EventBus, executor tools.CommandExecutor, fs persistence.FileSystem, wp services.WorkspacePolicy, dc deadCodeAnalyzer) *analysisManager {
	runner := toolchain.NewGoRunner(executor)
	m := &analysisManager{
		complexity: newComplexityAnalyzer(cache, sp, fs),
		dependency: newDependencyAnalyzer(runner, sp, bus, wp, fs),
		sequence:   newSequenceAnalyzer(executor, sp, idx),
		change:     newChangeAnalyzer(cache, executor),
		types:      newTypeManager(idx, cache, sp, fs),
		deadCode:   dc,

		refactor: newRefactorManager(sp),
		info:     &infoManager{SP: sp, Cache: cache, FS: fs, Events: bus, Runner: runner, Policy: wp},
		search:   &searchManager{SP: sp, FS: fs, Policy: wp},
		arch:     &architectureManager{SP: sp, Runner: runner, idx: idx},
	}

	m.health = &healthManager{
		SP:         sp,
		complexity: m.complexity,
		deadCode:   m.deadCode,
		Exec:       executor,
		Runner:     runner,
	}

	return m
}

// Delegated methods for registration

func (m *analysisManager) FindOrphanedSymbols(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.deadCode.FindOrphanedSymbols(ctx, args, hb)
}

func (m *analysisManager) AnalyzeComplexity(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.complexity.Analyze(ctx, args, hb)
}

func (m *analysisManager) GetPackageGraph(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.dependency.GetPackageGraph(ctx, args, hb)
}

func (m *analysisManager) AnalyzeSequenceFlow(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.sequence.AnalyzeSequenceFlow(ctx, args, hb)
}

func (m *analysisManager) SemanticDiff(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.change.SemanticDiff(ctx, args, hb)
}

func (m *analysisManager) ListImplementations(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.types.ListImplementations(ctx, args, hb)
}

func (m *analysisManager) FindUsages(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.types.FindUsages(ctx, args, hb)
}

func (m *analysisManager) ListSymbols(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.types.ListSymbols(ctx, args, hb)
}

func (m *analysisManager) GetTypeInfo(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.types.GetTypeInfo(ctx, args, hb)
}

func (m *analysisManager) FindDefinitions(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return m.types.FindDefinitions(ctx, args, hb)
}
