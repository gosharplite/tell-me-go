// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func TestResolver_Resolve_ToolNotFound_ReturnsSentinel(t *testing.T) {
	t.Parallel()

	reg := &mockToolRegistry{
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{
				{Name: "read_file"},
				{Name: "write_file"},
				{Name: "delete_file"},
			}
		},
	}

	r := newToolResolutionService(reg)
	_, err := r.Resolve(&llm.FunctionCall{Name: "nonexistent_tool"})

	if err == nil {
		t.Fatal("expected error for nonexistent tool, got nil")
	}

	if !errors.Is(err, ErrToolNotFound) {
		t.Errorf("expected errors.Is(err, ErrToolNotFound) to be true, got false; err=%v", err)
	}

	// Also verify the descriptive message is preserved
	if err.Error() == ErrToolNotFound.Error() {
		t.Error("expected error message to contain descriptive details beyond the sentinel")
	}
}
