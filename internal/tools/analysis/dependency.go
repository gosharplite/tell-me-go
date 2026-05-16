package analysis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"golang.org/x/sync/errgroup"
	"golang.org/x/tools/go/packages"
)

var filepathRelFn = filepath.Rel

type defaultDependencyAnalyzer struct {
	Runner    AnalysisGoRunner
	SP        domain_security.PolicyEvaluator
	Events    events.EventBus
	Policy    services.WorkspacePolicy
	modPrefix string
	modMu     sync.Mutex
}

func newDependencyAnalyzer(runner AnalysisGoRunner, sp domain_security.PolicyEvaluator, bus events.EventBus, wp services.WorkspacePolicy) *defaultDependencyAnalyzer {
	return &defaultDependencyAnalyzer{
		Runner: runner,
		SP:     sp,
		Events: bus,
		Policy: wp,
	}
}

func (a *defaultDependencyAnalyzer) GetPackageGraph(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	a.publishToolAction(ctx, "Analyzing package dependencies")

	format, _ := args["format"].(string)

	done := make(chan struct{})
	go a.startHeartbeat(hb, done)

	graph, err := a.buildGraph(ctx)
	close(done)
	if err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Error building graph: %v", err)}, nil
	}

	return tools.ToolResult{Text: a.renderGraph(graph, format)}, nil
}

func (a *defaultDependencyAnalyzer) buildGraph(ctx context.Context) (map[string][]string, error) {
	modPrefix, err := a.resolveModulePrefix(ctx)
	if err != nil {
		return nil, err
	}

	modRoot, err := a.Runner.GetModuleDir(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting module root: %w", err)
	}

	pkgPaths, err := a.listInternalPackages(modRoot)
	if err != nil {
		return nil, fmt.Errorf("listing packages: %w", err)
	}

	graph := make(map[string][]string)
	var mu sync.Mutex
	g, groupCtx := errgroup.WithContext(ctx)

	for _, p := range pkgPaths {
		path := p
		g.Go(func() error {
			imports, err := a.getImports(groupCtx, path, modPrefix)
			if err != nil {
				return err
			}

			rel, err := filepathRelFn(modRoot, path)
			if err != nil {
				return err
			}
			pkgImportPath := modPrefix
			if rel != "." {
				pkgImportPath = filepath.ToSlash(filepath.Join(modPrefix, rel))
			}

			mu.Lock()
			graph[pkgImportPath] = imports
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return graph, nil
}

func (a *defaultDependencyAnalyzer) publishToolAction(ctx context.Context, msg string) {
	if a.Events == nil {
		return
	}
	evt := events.SystemMessageEvent{
		Message: "[Tool Action] " + msg,
		Level:   "info",
	}
	if err := events.SafePublish(ctx, a.Events, evt); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			slog.Default().Error("event_publish_failed",
				slog.String("event_type", string(evt.Type())),
				slog.Any("error", err))
		}
	}
}

func (a *defaultDependencyAnalyzer) startHeartbeat(hb chan<- struct{}, done <-chan struct{}) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if hb != nil {
				select {
				case hb <- struct{}{}:
				default:
				}
			}
		}
	}
}

// containsGoFiles reports whether the directory at path contains at least one .go file.
func containsGoFiles(path string) (bool, error) {
	files, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".go") {
			return true, nil
		}
	}
	return false, nil
}

func (a *defaultDependencyAnalyzer) listInternalPackages(root string) ([]string, error) {
	var pkgs []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		if a.Policy.ShouldIgnoreDir(info.Name()) {
			return filepath.SkipDir
		}
		hasGo, err := containsGoFiles(path)
		if err != nil {
			return fmt.Errorf("checking for Go files in %s: %w", path, err)
		}
		if hasGo {
			pkgs = append(pkgs, path)
		}
		return nil
	})
	return pkgs, err
}

func (a *defaultDependencyAnalyzer) getImports(ctx context.Context, pkgPath string, modPrefix string) ([]string, error) {
	cfg := &packages.Config{
		Mode:    packages.NeedImports | packages.NeedFiles,
		Context: ctx,
		Dir:     pkgPath,
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, err
	}

	importMap := make(map[string]struct{})
	for _, pkg := range pkgs {
		for impPath := range pkg.Imports {
			if strings.HasPrefix(impPath, modPrefix) {
				importMap[impPath] = struct{}{}
			}
		}
	}

	var imports []string
	for imp := range importMap {
		imports = append(imports, imp)
	}
	sort.Strings(imports)
	return imports, nil
}

func (a *defaultDependencyAnalyzer) resolveModulePrefix(ctx context.Context) (string, error) {
	a.modMu.Lock()
	defer a.modMu.Unlock()

	if a.modPrefix != "" {
		return a.modPrefix, nil
	}

	mod, err := a.Runner.GetModulePath(ctx)
	if err != nil {
		return "", fmt.Errorf("getting module name: %w", err)
	}
	a.modPrefix = mod
	return a.modPrefix, nil
}

func (a *defaultDependencyAnalyzer) renderGraph(graph map[string][]string, format string) string {
	if format == "mermaid" {
		return generateMermaid(graph)
	}

	var sb strings.Builder
	sb.WriteString("Internal Package Dependency Graph:\n")

	pkgs := make([]string, 0, len(graph))
	for pkg := range graph {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)

	for _, pkg := range pkgs {
		internalImports := graph[pkg]
		if len(internalImports) > 0 {
			_, _ = fmt.Fprintf(&sb, "%s\n", pkg)
			for _, imp := range internalImports {
				_, _ = fmt.Fprintf(&sb, "  └── %s\n", imp)
			}
		} else {
			_, _ = fmt.Fprintf(&sb, "%s (no internal dependencies)\n", pkg)
		}
	}
	return sb.String()
}
