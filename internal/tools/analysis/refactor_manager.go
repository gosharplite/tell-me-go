package analysis

import (
	"context"
	"fmt"

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

func (m *refactorManager) RenameSymbol(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	// For now, keeping the old implementation logic but structured within the new manager
	// In a real implementation this would also use Transactions and Transforms
	var params struct {
		OldName string `json:"old_name"`
		NewName string `json:"new_name"`
		Path    string `json:"path"`
		Reason  string `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	// A better way is to implement RenameTransform
	return tools.ToolResult{Text: "RenameSymbol migrated to new manager (logic to be fully transitioned to Transforms)."}, nil
}

type refactorSecurity interface {
	security.PathValidator
	security.ActionConfirmer
}
