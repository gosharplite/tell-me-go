package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
)

// renamePlan describes a symbol rename operation within a package directory.
type renamePlan struct {
	OldName string
	NewName string
}

func (p *renamePlan) Description() string {
	return fmt.Sprintf("Rename %s → %s", p.OldName, p.NewName)
}

// renameTransform renames every *ast.Ident matching OldName to NewName across
// all loaded AST files in the transaction. It operates on the full set of
// files loaded into the transaction (typically all .go files in the target
// directory), so both the definition and all references are updated.
type renameTransform struct {
	Plan *renamePlan
}

func newRenameTransform(plan *renamePlan) *renameTransform {
	return &renameTransform{Plan: plan}
}

func (t *renameTransform) Apply(_ context.Context, _ *token.FileSet, files map[string]*ast.File) error {
	if t.Plan.OldName == t.Plan.NewName {
		return fmt.Errorf("old_name and new_name are identical: %q", t.Plan.OldName)
	}
	if t.Plan.NewName == "" {
		return fmt.Errorf("new_name must not be empty")
	}

	renamed := 0
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			ident, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if ident.Name == t.Plan.OldName {
				ident.Name = t.Plan.NewName
				renamed++
			}
			return true
		})
	}

	if renamed == 0 {
		return fmt.Errorf("symbol %q not found in any loaded file", t.Plan.OldName)
	}

	return nil
}
