package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

type defaultComplexityAnalyzer struct {
	Cache       *astCache
	SP          security.PathValidator
	skippedErrs []string // collects non-fatal errors from individual file processing
	mu          sync.Mutex
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

	a.skippedErrs = nil // reset from previous calls

	complexities, err := a.GatherComplexities(ctx, resolvedPath, hb)
	if err != nil {
		return tools.ToolResult{}, err
	}

	var text string
	if len(complexities) == 0 {
		text = "No Go functions found to analyze."
	} else {
		text = a.formatResults(complexities)
	}

	if len(a.skippedErrs) > 0 {
		text += "\n\n⚠️ Skipped " + strconv.Itoa(len(a.skippedErrs)) +
			" file(s) due to parse errors:\n" + strings.Join(a.skippedErrs, "\n")
	}

	return tools.ToolResult{Text: text}, nil
}

func (a *defaultComplexityAnalyzer) GatherComplexities(ctx context.Context, root string, hb chan<- struct{}) ([]funcComplexity, error) {
	g, gCtx := errgroup.WithContext(ctx)
	sem := semaphore.NewWeighted(a.getConcurrencyLimit())

	var complexities []funcComplexity
	var mu sync.Mutex
	count := 0

	walkFn := a.makeWalkFunc(g, gCtx, sem, hb, &complexities, &mu, &count)
	if err := filepath.Walk(root, walkFn); err != nil {
		return nil, err
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return complexities, nil
}

func (a *defaultComplexityAnalyzer) getConcurrencyLimit() int64 {
	limit := int64(runtime.NumCPU())
	if limit < 1 {
		limit = 1
	}
	return limit
}

func (a *defaultComplexityAnalyzer) makeWalkFunc(g *errgroup.Group, ctx context.Context, sem *semaphore.Weighted, hb chan<- struct{}, complexities *[]funcComplexity, mu *sync.Mutex, count *int) filepath.WalkFunc {
	return func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}

		*count++
		counter := *count
		path := filePath

		g.Go(func() error {
			return a.processFileTask(ctx, sem, path, hb, counter, complexities, mu)
		})
		return nil
	}
}

func (a *defaultComplexityAnalyzer) processFileTask(ctx context.Context, sem *semaphore.Weighted, path string, hb chan<- struct{}, counter int, complexities *[]funcComplexity, mu *sync.Mutex) error {
	if err := sem.Acquire(ctx, 1); err != nil {
		return err
	}
	defer sem.Release(1)

	if counter%10 == 0 && hb != nil {
		select {
		case hb <- struct{}{}:
		default:
		}
	}

	fileComplexities, err := a.analyzeFile(path)
	if err != nil {
		a.mu.Lock()
		a.skippedErrs = append(a.skippedErrs, fmt.Sprintf("%s: %v", path, err))
		a.mu.Unlock()
		return nil // soft-fail: individual file errors don't stop the entire analysis
	}
	if len(fileComplexities) > 0 {
		mu.Lock()
		*complexities = append(*complexities, fileComplexities...)
		mu.Unlock()
	}
	return nil
}

func (a *defaultComplexityAnalyzer) analyzeFile(filePath string) ([]funcComplexity, error) {
	f, fset, err := a.Cache.Get(filePath)
	if err != nil {
		return nil, fmt.Errorf("analyzeFile %s: %w", filePath, err)
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
	return fileComplexities, nil
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
