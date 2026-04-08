// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	stdctx "context"
	"testing"
)

func TestVersionCommand_Execute(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		version  string
		expected string
	}{
		{
			name:     "simple version",
			version:  "1.0.0",
			expected: "tell-me-go version 1.0.0\n",
		},
		{
			name:     "dev version",
			version:  "v1.91.0-dev",
			expected: "tell-me-go version v1.91.0-dev\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			cmdCtx := &context{
				Version: tt.version,
				Stdout:  &out,
			}
			cmd := newVersionCommand(cmdCtx)

			err := cmd.ExecuteContext(stdctx.Background())
			if err != nil {
				t.Fatalf("Execute() unexpected error: %v", err)
			}

			if out.String() != tt.expected {
				t.Errorf("Execute() got = %q, want %q", out.String(), tt.expected)
			}
		})
	}
}
