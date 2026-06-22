package openai

import (
	"errors"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestAppendPartsFromBlock_UnknownType_ReturnsSentinel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		blockType     string
		wantSentinel  bool   // true if errors.Is(err, errUnhandledBlockType) expected
		wantErrSubstr string // substring expected in error message
	}{
		{
			name:          "known_type_text_does_not_return_sentinel",
			blockType:     "text",
			wantSentinel:  false,
			wantErrSubstr: "", // no error expected at all
		},
		{
			name:          "unknown_type_returns_sentinel",
			blockType:     "future_block_type_xyz",
			wantSentinel:  true,
			wantErrSubstr: "future_block_type_xyz",
		},
		{
			name:          "another_unknown_type_returns_sentinel",
			blockType:     "experimental_block_v9",
			wantSentinel:  true,
			wantErrSubstr: "experimental_block_v9",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			content := &llm.Content{}
			cb := contentBlock{Type: tt.blockType}

			err := c.appendPartsFromBlock(content, cb)

			if tt.wantSentinel {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !errors.Is(err, errUnhandledBlockType) {
					t.Errorf("errors.Is(err, errUnhandledBlockType) = false; want true. err=%v", err)
				}
				if tt.wantErrSubstr != "" && !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErrSubstr)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for known type %q: %v", tt.blockType, err)
				}
			}
		})
	}
}

func TestAppendPartsFromBlock_ToolUseValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		blockType    string
		blockName    string
		blockID      string
		wantErr      bool
		wantSentinel error // sentinel to check with errors.Is, nil = no sentinel check
		wantSubstr   string
	}{
		{
			name:      "tool_use_with_name_and_id_ok",
			blockType: "tool_use",
			blockName: "test_tool",
			blockID:   "call_123",
			wantErr:   false,
		},
		{
			name:         "tool_use_with_name_missing_id_errors",
			blockType:    "tool_use",
			blockName:    "test_tool",
			blockID:      "",
			wantErr:      true,
			wantSentinel: errMissingToolID,
			wantSubstr:   `name="test_tool"`,
		},
		{
			name:      "tool_use_with_empty_name_and_id_silent_noop",
			blockType: "tool_use",
			blockName: "",
			blockID:   "",
			wantErr:   false,
		},
		{
			name:      "tool_use_with_empty_name_but_id_silent_noop",
			blockType: "tool_use",
			blockName: "",
			blockID:   "call_456",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			content := &llm.Content{}
			cb := contentBlock{
				Type: tt.blockType,
				Name: tt.blockName,
				ID:   tt.blockID,
			}

			err := c.appendPartsFromBlock(content, cb)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantSentinel != nil && !errors.Is(err, tt.wantSentinel) {
					t.Errorf("errors.Is(err, %v) = false; want true. err=%v", tt.wantSentinel, err)
				}
				if tt.wantSubstr != "" && !strings.Contains(err.Error(), tt.wantSubstr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantSubstr)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
