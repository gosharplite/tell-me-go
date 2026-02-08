package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

type ChangeAnalyzer struct {
	Cache *ASTCache
	Exec  CommandExecutor
}

func NewChangeAnalyzer(cache *ASTCache, exec CommandExecutor) *ChangeAnalyzer {
	return &ChangeAnalyzer{
		Cache: cache,
		Exec:  exec,
	}
}

func (a *ChangeAnalyzer) SemanticDiff(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Target string `json:"target"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	target := params.Target

	// 1. Get stats and summary
	statOut, _ := a.Exec.CombinedOutput(ctx, "git", "diff", "--stat", target)
	summaryOut, _ := a.Exec.CombinedOutput(ctx, "git", "diff", "--summary", target)

	var sb strings.Builder
	sb.WriteString("Semantic Diff Summary:\n\n")
	sb.WriteString("File Statistics:\n")
	sb.WriteString(string(statOut))
	sb.WriteString("\nChange Summary:\n")
	sb.WriteString(string(summaryOut))

	// 2. Logical Analysis
	sb.WriteString("\nLogical Code Changes:\n")

	// Get list of changed .go files
	filesOut, err := a.Exec.CombinedOutput(ctx, "git", "diff", "--name-only", target)
	if err != nil {
		return tools.ToolResult{Text: sb.String() + "\n(Could not perform logical analysis)"}, nil
	}

	changedFiles := strings.Split(strings.TrimSpace(string(filesOut)), "\n")
	fset := token.NewFileSet()

	for _, relPath := range changedFiles {
		if filepath.Ext(relPath) != ".go" || strings.Contains(relPath, "vendor/") {
			continue
		}

		// Get current AST
		currAST, _, err := a.Cache.Get(relPath)
		if err != nil {
			continue // Skip unparsable current files
		}

		// Get target AST (base)
		var baseAST *ast.File
		baseContent, err := a.Exec.Output(ctx, "git", "show", target+":"+relPath)
		if err == nil {
			baseAST, _ = parser.ParseFile(fset, relPath, baseContent, parser.ParseComments)
		}

		var changes []string
		if baseAST == nil {
			// Entirely new file
			for _, d := range currAST.Decls {
				key := GetDeclKey(d)
				if key != "unknown" {
					changes = append(changes, "Added: "+key)
				}
			}
		} else {
			changes = CompareASTs(baseAST, currAST)
		}
		if len(changes) > 0 {
			sb.WriteString(fmt.Sprintf("\n[%s]\n", relPath))
			for _, ch := range changes {
				sb.WriteString(fmt.Sprintf("  - %s\n", ch))
			}
		}
	}

	return tools.ToolResult{Text: sb.String()}, nil
}
