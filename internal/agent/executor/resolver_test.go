// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
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

	if !errors.Is(err, errToolNotFound) {
		t.Errorf("expected errors.Is(err, errToolNotFound) to be true, got false; err=%v", err)
	}

	// Also verify the descriptive message is preserved
	if err.Error() == errToolNotFound.Error() {
		t.Error("expected error message to contain descriptive details beyond the sentinel")
	}
}

func TestResolver_NilCall_ReturnsError(t *testing.T) {
	t.Parallel()

	reg := &mockToolRegistry{
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{
				{Name: "read_file"},
			}
		},
	}

	r := newToolResolutionService(reg)

	tests := []struct {
		name      string
		call      *llm.FunctionCall
		wantErr   string
		expectNil bool
	}{
		{
			name:      "nil_call",
			call:      nil,
			wantErr:   "cannot resolve nil function call",
			expectNil: true,
		},
		{
			name:      "non_nil_call_unaffected",
			call:      &llm.FunctionCall{Name: "read_file"},
			wantErr:   "",
			expectNil: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tool, err := r.Resolve(tt.call)

			if tt.wantErr != "" {
				assert.Error(t, err)
				assert.Nil(t, tool)
				assert.Equal(t, tt.wantErr, err.Error())
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, tool)
				assert.Equal(t, "read_file", tool.Name)
			}
		})
	}
}
