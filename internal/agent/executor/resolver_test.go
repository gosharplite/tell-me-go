// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolver_Resolve_ToolNotFound_ReturnsSentinel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		toolName       string
		registryTools  []string
		wantSentinel   bool
		wantSuggestion string // empty means "no did-you-mean expected"
	}{
		{
			name:           "far_mismatch_no_suggestion",
			toolName:       "nonexistent_tool",
			registryTools:  []string{"read_file", "write_file", "delete_file"},
			wantSentinel:   true,
			wantSuggestion: "",
		},
		{
			name:           "close_mismatch_with_suggestion",
			toolName:       "read_files",
			registryTools:  []string{"read_file", "write_file", "delete_file"},
			wantSentinel:   true,
			wantSuggestion: "read_file",
		},
		{
			name:           "case_insensitive_close_match",
			toolName:       "READ_FILE",
			registryTools:  []string{"read_file", "write_file", "delete_file"},
			wantSentinel:   true,
			wantSuggestion: "read_file",
		},
		{
			name:           "very_distant_no_suggestion",
			toolName:       "zzzzzzzzz",
			registryTools:  []string{"read_file", "write_file", "delete_file"},
			wantSentinel:   true,
			wantSuggestion: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reg := &mockToolRegistry{
				getDeclarationsFn: func() []*tools.ToolDeclaration {
					decls := make([]*tools.ToolDeclaration, len(tt.registryTools))
					for i, name := range tt.registryTools {
						decls[i] = &tools.ToolDeclaration{Name: name}
					}
					return decls
				},
			}

			r := newToolResolutionService(reg)
			_, err := r.Resolve(&llm.FunctionCall{Name: tt.toolName})

			require.Error(t, err)
			require.True(t, errors.Is(err, errToolNotFound), "must wrap errToolNotFound sentinel")

			if tt.wantSuggestion != "" {
				expected := fmt.Sprintf("; did you mean %q?", tt.wantSuggestion)
				assert.Contains(t, err.Error(), expected,
					"error should contain suggestion for close match")
			} else {
				assert.NotContains(t, err.Error(), "did you mean",
					"error should NOT contain suggestion for distant mismatch")
			}

			// Verify descriptive details are present beyond the sentinel
			assert.NotEqual(t, errToolNotFound.Error(), err.Error(),
				"error should contain descriptive details beyond the sentinel")
		})
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
