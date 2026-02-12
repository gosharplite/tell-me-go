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
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
)

type changeAnalyzer struct {
	Cache *astCache
	Exec  tools.CommandExecutor
}

func newChangeAnalyzer(cache *astCache, exec tools.CommandExecutor) *changeAnalyzer {
	return &changeAnalyzer{
		Cache: cache,
		Exec:  exec,
	}
}

func (a *changeAnalyzer) SemanticDiff(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Target string `json:"target"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	metadata, changedFiles, err := a.getDiffMetadata(ctx, params.Target)

	var sb strings.Builder
	sb.WriteString("Semantic Diff Summary:\n\n")
	sb.WriteString(metadata)

	if err != nil {
		return tools.ToolResult{Text: sb.String() + "\n(Could not perform logical analysis)"}, nil
	}

	sb.WriteString("\nLogical Code Changes:\n")
	fset := token.NewFileSet()
	for _, relPath := range changedFiles {
		if a.isGoFile(relPath) {
			changes, _ := a.analyzeFileChange(ctx, params.Target, relPath, fset)
			a.renderChanges(&sb, relPath, changes)
		}
	}

	return tools.ToolResult{Text: sb.String()}, nil
}

func (a *changeAnalyzer) getDiffMetadata(ctx context.Context, target string) (string, []string, error) {
	statOut, _ := a.Exec.CombinedOutput(ctx, "git", "diff", "--stat", target)
	summaryOut, _ := a.Exec.CombinedOutput(ctx, "git", "diff", "--summary", target)

	var sb strings.Builder
	sb.WriteString("File Statistics:\n")
	sb.WriteString(string(statOut))
	sb.WriteString("\nChange Summary:\n")
	sb.WriteString(string(summaryOut))

	filesOut, err := a.Exec.CombinedOutput(ctx, "git", "diff", "--name-only", target)
	if err != nil {
		return sb.String(), nil, err
	}

	filesRaw := strings.TrimSpace(string(filesOut))
	if filesRaw == "" {
		return sb.String(), nil, nil
	}
	changedFiles := strings.Split(filesRaw, "\n")

	return sb.String(), changedFiles, nil
}

func (a *changeAnalyzer) analyzeFileChange(ctx context.Context, target, relPath string, fset *token.FileSet) ([]string, error) {
	// Get current AST
	currAST, _, err := a.Cache.Get(relPath)
	if err != nil {
		return nil, err
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
			key := getDeclKey(d)
			if key != "unknown" {
				changes = append(changes, "Added: "+key)
			}
		}
	} else {
		changes = compareASTs(baseAST, currAST)
	}
	return changes, nil
}

func (a *changeAnalyzer) isGoFile(relPath string) bool {
	return filepath.Ext(relPath) == ".go" && !strings.Contains(relPath, "vendor/")
}

func (a *changeAnalyzer) renderChanges(sb *strings.Builder, relPath string, changes []string) {
	if len(changes) > 0 {
		sb.WriteString(fmt.Sprintf("\n[%s]\n  - %s\n", relPath, strings.Join(changes, "\n  - ")))
	}
}
