package analysis

import (
	"context"
	"go/ast"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenamePlanDescription(t *testing.T) {
	t.Parallel()
	plan := &renamePlan{OldName: "OldFunc", NewName: "NewFunc"}
	desc := plan.Description()
	expected := "Rename OldFunc → NewFunc"
	if desc != expected {
		t.Errorf("Description() = %q, want %q", desc, expected)
	}
}

func TestRenameTransform_ErrorContext(t *testing.T) {
	t.Parallel()

	t.Run("identical_names_error", func(t *testing.T) {
		t.Parallel()
		plan := &renamePlan{OldName: "Foo", NewName: "Foo"}
		tr := newRenameTransform(plan)
		err := tr.Apply(context.TODO(), nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "identical")
		assert.Contains(t, err.Error(), "Foo")
	})

	t.Run("empty_new_name_error", func(t *testing.T) {
		t.Parallel()
		plan := &renamePlan{OldName: "Foo", NewName: ""}
		tr := newRenameTransform(plan)
		err := tr.Apply(context.TODO(), nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must not be empty")
	})

	t.Run("symbol_not_found_lists_scanned_files", func(t *testing.T) {
		t.Parallel()
		plan := &renamePlan{OldName: "NonExistent", NewName: "Whatever"}
		tr := newRenameTransform(plan)

		files := map[string]*ast.File{
			"/tmp/pkg/alpha.go": {Name: ast.NewIdent("p")},
			"/tmp/pkg/beta.go":  {Name: ast.NewIdent("p")},
			"/tmp/pkg/gamma.go": {Name: ast.NewIdent("p")},
		}

		err := tr.Apply(context.TODO(), nil, files)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rename NonExistent→Whatever")
		assert.Contains(t, err.Error(), "not found in 3 files")
		assert.Contains(t, err.Error(), "alpha.go")
		assert.Contains(t, err.Error(), "beta.go")
		assert.Contains(t, err.Error(), "gamma.go")
	})
}
