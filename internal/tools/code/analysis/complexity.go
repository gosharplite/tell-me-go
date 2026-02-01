package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/tools/code/astutil"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type ComplexityAnalyzer struct {
	Cache *astutil.ASTCache
	SP    types.SecurityProvider
}

func NewComplexityAnalyzer(cache *astutil.ASTCache, sp types.SecurityProvider) *ComplexityAnalyzer {
	return &ComplexityAnalyzer{
		Cache: cache,
		SP:    sp,
	}
}

func (a *ComplexityAnalyzer) Analyze(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.Path
	resolvedPath, err := a.SP.IsPathSafe(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	var results []string

	err = filepath.Walk(resolvedPath, func(filePath string, info os.FileInfo, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}

		f, fset, err := a.Cache.Get(filePath)
		if err != nil {
			return nil
		}

		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok {
				complexity := astutil.CalculateComplexity(fd)
				funcName := fd.Name.Name
				if fd.Recv != nil {
					recvType := astutil.ExprToString(fd.Recv.List[0].Type)
					funcName = fmt.Sprintf("(%s).%s", recvType, funcName)
				}
				results = append(results, fmt.Sprintf("%s:%d: %s - Complexity: %d", filePath, fset.Position(fd.Pos()).Line, funcName, complexity))
			}
		}
		return nil
	})

	if err != nil {
		return types.ToolResult{}, err
	}
	if len(results) == 0 {
		return types.ToolResult{Text: "No Go functions found to analyze."}, nil
	}

	// Sort by complexity descending
	sort.Slice(results, func(i, j int) bool {
		var ci, cj int
		fmt.Sscanf(results[i], "%*[^:]: %*d: %*s - Complexity: %d", &ci)
		fmt.Sscanf(results[j], "%*[^:]: %*d: %*s - Complexity: %d", &cj)
		return ci > cj
	})

	if len(results) > 100 {
		results = append(results[:100], "... (truncated)")
	}

	return types.ToolResult{Text: "Cyclomatic Complexity Analysis (Top 100):\n" + strings.Join(results, "\n")}, nil
}
