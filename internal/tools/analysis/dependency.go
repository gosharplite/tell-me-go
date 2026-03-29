package analysis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type defaultDependencyAnalyzer struct {
	Exec   tools.CommandExecutor
	SP     domain_security.PolicyEvaluator
	Events events.EventBus
}

func newDependencyAnalyzer(exec tools.CommandExecutor, sp domain_security.PolicyEvaluator, bus events.EventBus) *defaultDependencyAnalyzer {
	return &defaultDependencyAnalyzer{
		Exec:   exec,
		SP:     sp,
		Events: bus,
	}
}

func (a *defaultDependencyAnalyzer) GetPackageGraph(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	if a.Events != nil {
		evt := events.SystemMessageEvent{
			Message: "[Tool Action] Analyzing package dependencies",
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

	format, _ := args["format"].(string)

	// Heartbeat while building graph
	done := make(chan struct{})
	go func() {
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
	}()

	graph, err := a.buildGraph(ctx)
	close(done)
	if err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Error building graph: %v", err)}, nil
	}

	if format == "mermaid" {
		return tools.ToolResult{Text: generateMermaid(graph)}, nil
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

	return tools.ToolResult{Text: sb.String()}, nil
}

func (a *defaultDependencyAnalyzer) buildGraph(ctx context.Context) (map[string][]string, error) {
	out, err := a.Exec.CombinedOutput(ctx, "go", "list", "-f", "{{.ImportPath}} -> {{.Imports}}", "./...")
	if err != nil {
		return nil, fmt.Errorf("listing packages: %w (output: %s)", err, string(out))
	}

	// Get module name to filter for internal imports
	modOut, _ := a.Exec.Output(ctx, "go", "list", "-m")
	modName := strings.TrimSpace(string(modOut))

	graph := make(map[string][]string)
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		parts := strings.Split(line, " -> ")
		if len(parts) != 2 {
			continue
		}
		pkg := parts[0]
		importsRaw := strings.Trim(parts[1], "[]")
		imports := strings.Fields(importsRaw)

		var internalImports []string
		for _, imp := range imports {
			if strings.HasPrefix(imp, modName) {
				internalImports = append(internalImports, imp)
			}
		}
		graph[pkg] = internalImports
	}
	return graph, nil
}
