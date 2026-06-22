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
