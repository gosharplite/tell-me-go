package analysis

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/ui/colors"
)

type DependencyAnalyzer struct {
	Exec CommandExecutor
	SP   security.SecurityProvider
}

func NewDependencyAnalyzer(exec CommandExecutor, sp security.SecurityProvider) *DependencyAnalyzer {
	return &DependencyAnalyzer{
		Exec: exec,
		SP:   sp,
	}
}

func (a *DependencyAnalyzer) GetPackageGraph(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	func() {
		a.SP.TerminalLock()
		defer a.SP.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "%s[Tool Action] Analyzing package dependencies%s\n", colors.ColorCyan, colors.ColorReset)
	}()

	format, _ := args["format"].(string)

	graph, err := a.buildGraph(ctx)
	if err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Error building graph: %v", err)}, nil
	}

	if format == "mermaid" {
		return tools.ToolResult{Text: GenerateMermaid(graph)}, nil
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
			sb.WriteString(fmt.Sprintf("%s\n", pkg))
			for _, imp := range internalImports {
				sb.WriteString(fmt.Sprintf("  └── %s\n", imp))
			}
		} else {
			sb.WriteString(fmt.Sprintf("%s (no internal dependencies)\n", pkg))
		}
	}

	return tools.ToolResult{Text: sb.String()}, nil
}

func (a *DependencyAnalyzer) buildGraph(ctx context.Context) (map[string][]string, error) {
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
