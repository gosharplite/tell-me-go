package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

type defaultComplexityAnalyzer struct {
	Cache *astCache
	SP    security.PathValidator
}

func newComplexityAnalyzer(cache *astCache, sp security.PathValidator) *defaultComplexityAnalyzer {
	return &defaultComplexityAnalyzer{
		Cache: cache,
		SP:    sp,
	}
}

type funcComplexity struct {
	Line       int
	Name       string
	Complexity int
	FilePath   string
}

func (a *defaultComplexityAnalyzer) Analyze(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	resolvedPath, err := a.SP.IsPathSafe(params.Path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	complexities, err := a.GatherComplexities(ctx, resolvedPath, hb)
	if err != nil {
		return tools.ToolResult{}, err
	}

	if len(complexities) == 0 {
		return tools.ToolResult{Text: "No Go functions found to analyze."}, nil
	}

	return tools.ToolResult{Text: a.formatResults(complexities)}, nil
}

func (a *defaultComplexityAnalyzer) GatherComplexities(ctx context.Context, root string, hb chan<- struct{}) ([]funcComplexity, error) {
	g, gCtx := errgroup.WithContext(ctx)
	limit := int64(runtime.NumCPU())
	if limit < 1 {
		limit = 1
	}
	sem := semaphore.NewWeighted(limit)

	var complexities []funcComplexity
	var mu sync.Mutex

	count := 0
	err := filepath.Walk(root, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		select {
		case <-gCtx.Done():
			return gCtx.Err()
		default:
		}
		if info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}

		path := filePath
		count++
		c := count
		g.Go(func() error {
			if err := sem.Acquire(gCtx, 1); err != nil {
				return err
			}
			defer sem.Release(1)

			if c%10 == 0 && hb != nil {
				select {
				case hb <- struct{}{}:
				default:
				}
			}

			fileComplexities := a.analyzeFile(path)
			if len(fileComplexities) > 0 {
				mu.Lock()
				complexities = append(complexities, fileComplexities...)
				mu.Unlock()
			}
			return nil
		})
		return nil
	})

	if err != nil {
		return nil, err
	}

	if waitErr := g.Wait(); waitErr != nil {
		return nil, waitErr
	}

	return complexities, nil
}

func (a *defaultComplexityAnalyzer) analyzeFile(filePath string) []funcComplexity {
	f, fset, err := a.Cache.Get(filePath)
	if err != nil {
		return nil
	}

	var fileComplexities []funcComplexity
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			complexity := calculateComplexity(fd)
			funcName := fd.Name.Name
			if fd.Recv != nil {
				recvType := exprToString(fd.Recv.List[0].Type)
				funcName = fmt.Sprintf("(%s).%s", recvType, funcName)
			}
			fileComplexities = append(fileComplexities, funcComplexity{
				Line:       fset.Position(fd.Pos()).Line,
				Name:       funcName,
				Complexity: complexity,
				FilePath:   filePath,
			})
		}
	}
	return fileComplexities
}

func (a *defaultComplexityAnalyzer) formatResults(complexities []funcComplexity) string {
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
