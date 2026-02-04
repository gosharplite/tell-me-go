package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/code/astutil"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

type ComplexityAnalyzer struct {
	Cache *astutil.ASTCache
	SP    security.SecurityProvider
}

func NewComplexityAnalyzer(cache *astutil.ASTCache, sp security.SecurityProvider) *ComplexityAnalyzer {
	return &ComplexityAnalyzer{
		Cache: cache,
		SP:    sp,
	}
}

type FuncComplexity struct {
	Line       int
	Name       string
	Complexity int
	FilePath   string
}

func (a *ComplexityAnalyzer) Analyze(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	resolvedPath, err := a.SP.IsPathSafe(params.Path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	complexities, err := a.GatherComplexities(ctx, resolvedPath)
	if err != nil {
		return tools.ToolResult{}, err
	}

	if len(complexities) == 0 {
		return tools.ToolResult{Text: "No Go functions found to analyze."}, nil
	}

	return tools.ToolResult{Text: a.formatResults(complexities)}, nil
}

func (a *ComplexityAnalyzer) GatherComplexities(ctx context.Context, root string) ([]FuncComplexity, error) {
	var complexities []FuncComplexity
	err := filepath.Walk(root, func(filePath string, info os.FileInfo, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}

		fileComplexities := a.analyzeFile(filePath)
		complexities = append(complexities, fileComplexities...)
		return nil
	})
	return complexities, err
}

func (a *ComplexityAnalyzer) analyzeFile(filePath string) []FuncComplexity {
	f, fset, err := a.Cache.Get(filePath)
	if err != nil {
		return nil
	}

	var fileComplexities []FuncComplexity
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			complexity := astutil.CalculateComplexity(fd)
			funcName := fd.Name.Name
			if fd.Recv != nil {
				recvType := astutil.ExprToString(fd.Recv.List[0].Type)
				funcName = fmt.Sprintf("(%s).%s", recvType, funcName)
			}
			fileComplexities = append(fileComplexities, FuncComplexity{
				Line:       fset.Position(fd.Pos()).Line,
				Name:       funcName,
				Complexity: complexity,
				FilePath:   filePath,
			})
		}
	}
	return fileComplexities
}

func (a *ComplexityAnalyzer) formatResults(complexities []FuncComplexity) string {
	// Sort by complexity descending
	sort.Slice(complexities, func(i, j int) bool {
		if complexities[i].Complexity != complexities[j].Complexity {
			return complexities[i].Complexity > complexities[j].Complexity
		}
		return complexities[i].Name < complexities[j].Name
	})

	limit := 100
	truncated := false
	if len(complexities) > limit {
		complexities = complexities[:limit]
		truncated = true
	}

	var results []string
	for _, c := range complexities {
		results = append(results, fmt.Sprintf("%s:%d: %s - Complexity: %d", c.FilePath, c.Line, c.Name, c.Complexity))
	}
	if truncated {
		results = append(results, "... (truncated)")
	}

	return "Cyclomatic Complexity Analysis (Top 100):\n" + strings.Join(results, "\n")
}
