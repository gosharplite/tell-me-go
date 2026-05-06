package analysis

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type refactorManager struct {
	SP refactorSecurity
}

func newRefactorManager(sp refactorSecurity) *refactorManager {
	return &refactorManager{SP: sp}
}

func (m *refactorManager) MoveDefinition(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Symbol  string `json:"symbol"`
		SrcFile string `json:"src_file"`
		DstFile string `json:"dst_file"`
		Reason  string `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	plan := &movePlan{
		Symbol:  params.Symbol,
		SrcFile: params.SrcFile,
		DstFile: params.DstFile,
	}

	// Security Check
	resolvedSrc, err := m.SP.IsPathWritable(plan.SrcFile)
	if err != nil {
		return tools.ToolResult{}, err
	}
	resolvedDst, err := m.SP.IsPathWritable(plan.DstFile)
	if err != nil {
		return tools.ToolResult{}, err
	}
	plan.SrcFile = resolvedSrc
	plan.DstFile = resolvedDst

	tx := newTransaction()
	if _, err := tx.LoadFile(plan.SrcFile); err != nil {
		return tools.ToolResult{}, err
	}
	if _, err := tx.LoadFile(plan.DstFile); err != nil {
		// Handle non-existent destination
		// For simplicity in this implementation we assume it exists or fail
		// Real implementation should handle it
		return tools.ToolResult{}, err
	}

	tx.Add(newMoveTransform(plan))
	tx.Add(newImportCleanupTransform(plan.SrcFile))
	tx.Add(newImportCleanupTransform(plan.DstFile))

	if err := tx.Commit(ctx); err != nil {
		return tools.ToolResult{}, err
	}

	return tools.ToolResult{Text: fmt.Sprintf("Successfully moved %s from %s to %s", plan.Symbol, plan.SrcFile, plan.DstFile)}, nil
}

// loadGoFilesForRename discovers all Go source files in resolvedDir, loads
// them into a new transaction, and returns the file list and transaction.
// Callers own the responsibility of adding transforms and committing.
func (m *refactorManager) loadGoFilesForRename(resolvedDir string) ([]string, *transaction, error) {
	goFiles, err := filepath.Glob(filepath.Join(resolvedDir, "*.go"))
	if err != nil {
		return nil, nil, fmt.Errorf("glob %s: %w", resolvedDir, err)
	}
	if len(goFiles) == 0 {
		return nil, nil, fmt.Errorf("no .go files found in %s", resolvedDir)
	}

	tx := newTransaction()
	for _, f := range goFiles {
		if _, err := tx.LoadFile(f); err != nil {
			return nil, nil, fmt.Errorf("load %s: %w", filepath.Base(f), err)
		}
	}
	return goFiles, tx, nil
}

func (m *refactorManager) RenameSymbol(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		OldName string `json:"old_name"`
		NewName string `json:"new_name"`
		Path    string `json:"path"`
		Reason  string `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}
	if params.Path == "" {
		params.Path = "."
	}

	// Resolve and validate the target directory.
	resolvedDir, err := m.SP.IsPathWritable(params.Path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	// Discover and load all Go source files into a transaction.
	goFiles, tx, err := m.loadGoFilesForRename(resolvedDir)
	if err != nil {
		return tools.ToolResult{}, err
	}

	plan := &renamePlan{OldName: params.OldName, NewName: params.NewName}
	tx.Add(newRenameTransform(plan))
	for _, f := range goFiles {
		tx.Add(newImportCleanupTransform(f))
	}

	if err := tx.Commit(ctx); err != nil {
		return tools.ToolResult{}, err
	}

	// Build a compact list of filenames for the result.
	baseNames := make([]string, len(goFiles))
	for i, f := range goFiles {
		baseNames[i] = filepath.Base(f)
	}
	return tools.ToolResult{
		Text: fmt.Sprintf("Renamed %s → %s across %d files (%s).",
			params.OldName, params.NewName, len(goFiles), strings.Join(baseNames, ", ")),
	}, nil
}

type refactorSecurity interface {
	security.PathValidator
	security.ActionConfirmer
}
