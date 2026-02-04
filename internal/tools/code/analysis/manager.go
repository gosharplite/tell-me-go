package analysis

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/code/astutil"
	"github.com/gosharplite/tell-me-go/internal/tools/code/index"
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

type AnalysisManager struct {
	Complexity IComplexityAnalyzer
	Dependency IDependencyAnalyzer
	Sequence   ISequenceAnalyzer
	Change     IChangeAnalyzer
	Types      ITypeManager
}

func NewAnalysisManager(idx index.SymbolIndex, cache *astutil.ASTCache, sp security.SecurityProvider) *AnalysisManager {
	exec := &RealExecutor{}
	return &AnalysisManager{
		Complexity: NewComplexityAnalyzer(cache, sp),
		Dependency: NewDependencyAnalyzer(exec, sp),
		Sequence:   &SequenceAnalyzer{SP: sp},
		Change:     NewChangeAnalyzer(cache, exec),
		Types:      NewTypeManager(idx, cache, sp),
	}
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
