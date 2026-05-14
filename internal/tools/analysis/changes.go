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
)

type defaultChangeAnalyzer struct {
	Cache *astCache
	Exec  tools.CommandExecutor
}

func newChangeAnalyzer(cache *astCache, exec tools.CommandExecutor) *defaultChangeAnalyzer {
	return &defaultChangeAnalyzer{
		Cache: cache,
		Exec:  exec,
	}
}

func (a *defaultChangeAnalyzer) SemanticDiff(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Target string `json:"target"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
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
	for i, relPath := range changedFiles {
		if i%5 == 0 && hb != nil {
			select {
			case hb <- struct{}{}:
			default:
			}
		}
		if a.isGoFile(relPath) {
			changes, changeErr := a.analyzeFileChange(ctx, params.Target, relPath, fset)
			if changeErr != nil {
				// Soft-fail: include error in output but continue
				a.renderChanges(&sb, relPath, []string{"(analysis error: " + changeErr.Error() + ")"})
			} else {
				a.renderChanges(&sb, relPath, changes)
			}
		}
	}

	return tools.ToolResult{Text: sb.String()}, nil
}

func (a *defaultChangeAnalyzer) getDiffMetadata(ctx context.Context, target string) (string, []string, error) {
	var sb strings.Builder

	statOut, statErr := a.Exec.CombinedOutput(ctx, "git", "diff", "--stat", target)
	sb.WriteString("File Statistics:\n")
	if statErr != nil {
		fmt.Fprintf(&sb, "(stat failed: %s)\n", statErr.Error())
	} else {
		sb.WriteString(string(statOut))
	}

	summaryOut, summaryErr := a.Exec.CombinedOutput(ctx, "git", "diff", "--summary", target)
	sb.WriteString("\nChange Summary:\n")
	if summaryErr != nil {
		fmt.Fprintf(&sb, "(summary failed: %s)\n", summaryErr.Error())
	} else {
		sb.WriteString(string(summaryOut))
	}

	filesOut, err := a.Exec.CombinedOutput(ctx, "git", "diff", "--name-only", target)
	if err != nil {
		return sb.String(), nil, fmt.Errorf("getDiffMetadata --name-only: %w", err)
	}

	filesRaw := strings.TrimSpace(string(filesOut))
	if filesRaw == "" {
		return sb.String(), nil, nil
	}
	changedFiles := strings.Split(filesRaw, "\n")

	return sb.String(), changedFiles, nil
}

func (a *defaultChangeAnalyzer) analyzeFileChange(ctx context.Context, target, relPath string, fset *token.FileSet) ([]string, error) {
	// Get current AST
	currAST, _, err := a.Cache.Get(relPath)
	if err != nil {
		return nil, fmt.Errorf("analyzeFileChange %s: %w", relPath, err)
	}

	// Get target AST (base)
	var baseAST *ast.File
	var baseParseErr error
	baseContent, gitErr := a.Exec.Output(ctx, "git", "show", target+":"+relPath)
	if gitErr == nil {
		baseAST, baseParseErr = parser.ParseFile(fset, relPath, baseContent, parser.ParseComments)
	}

	var changes []string
	if gitErr != nil {
		// File didn't exist in target — entirely new file
		for _, d := range currAST.Decls {
			key := getDeclKey(d)
			if key != "unknown" {
				changes = append(changes, "Added: "+key)
			}
		}
	} else if baseParseErr != nil {
		// Could not parse base file — treat as unanalyzable
		return nil, fmt.Errorf("could not analyze base version of %s: %w", relPath, baseParseErr)
	} else {
		changes = compareASTs(baseAST, currAST)
	}
	return changes, nil
}

func (a *defaultChangeAnalyzer) isGoFile(relPath string) bool {
	return filepath.Ext(relPath) == ".go" && !strings.Contains(relPath, "vendor/")
}

func (a *defaultChangeAnalyzer) renderChanges(sb *strings.Builder, relPath string, changes []string) {
	if len(changes) > 0 {
		_, _ = fmt.Fprintf(sb, "\n[%s]\n  - %s\n", relPath, strings.Join(changes, "\n  - "))
	}
}
