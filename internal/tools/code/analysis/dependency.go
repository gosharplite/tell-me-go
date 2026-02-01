package analysis

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/types"
)

type DependencyAnalyzer struct {
	Exec CommandExecutor
	SP   types.SecurityProvider
}

func NewDependencyAnalyzer(exec CommandExecutor, sp types.SecurityProvider) *DependencyAnalyzer {
	return &DependencyAnalyzer{
		Exec: exec,
		SP:   sp,
	}
}

func (a *DependencyAnalyzer) GetPackageGraph(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	func() {
		a.SP.TerminalLock()
		defer a.SP.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Analyzing package dependencies\033[0m\n")
	}()

	out, err := a.Exec.CombinedOutput(ctx, "go", "list", "-f", "{{.ImportPath}} -> {{.Imports}}", "./...")
	if err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Error listing packages: %v\nOutput: %s", err, string(out))}, nil
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var sb strings.Builder
	sb.WriteString("Internal Package Dependency Graph:\n")

	// Get module name to filter for internal imports
	modOut, _ := a.Exec.Output(ctx, "go", "list", "-m")
	modName := strings.TrimSpace(string(modOut))

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

		if len(internalImports) > 0 {
			sb.WriteString(fmt.Sprintf("%s\n", pkg))
			for _, imp := range internalImports {
				sb.WriteString(fmt.Sprintf("  └── %s\n", imp))
			}
		} else {
			sb.WriteString(fmt.Sprintf("%s (no internal dependencies)\n", pkg))
		}
	}

	return types.ToolResult{Text: sb.String()}, nil
}
