// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package system

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestShellTool_UTF8SafeTruncation(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	tool := NewShellTool(sm)

	// "世界" is 6 bytes. "世" is 3 bytes, "界" is 3 bytes.

	ctx := context.Background()
	// Use sh -c to echo the multi-byte string exactly.
	// In UTF-8: 世 = \xe4\xb8\x96, 界 = \xe7\x95\x8c
	args := map[string]interface{}{
		"command": `sh -c 'printf "世界"'`,
		"reason":  "testing utf8 truncation",
	}

	tests := []struct {
		name          string
		maxOutput     int
		expectedPart  string
		forbiddenPart string
		wantTruncated bool
	}{
		{
			name:          "no truncation",
			maxOutput:     10,
			expectedPart:  "世界",
			forbiddenPart: "",
			wantTruncated: false,
		},
		{
			name:          "exact boundary",
			maxOutput:     7, // 6 bytes for "世界" + 1 byte for newline added by executor
			expectedPart:  "世界",
			forbiddenPart: "",
			wantTruncated: false,
		},
		{
			name:          "truncate middle of char",
			maxOutput:     5,
			expectedPart:  "世",
			forbiddenPart: "界",
			wantTruncated: true,
		},
		{
			name:          "truncate to first char",
			maxOutput:     3,
			expectedPart:  "世",
			forbiddenPart: "界",
			wantTruncated: true,
		},
		{
			name:          "truncate before first char",
			maxOutput:     2,
			expectedPart:  "",
			forbiddenPart: "世",
			wantTruncated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool.maxOutput = tt.maxOutput
			res, err := tool.ExecuteCommand(ctx, args)
			if err != nil {
				t.Fatalf("ExecuteCommand failed: %v", err)
			}

			if tt.expectedPart != "" && !strings.Contains(res.Text, tt.expectedPart) {
				t.Errorf("expected output to contain %q, got %q", tt.expectedPart, res.Text)
			}
			if tt.forbiddenPart != "" && strings.Contains(res.Text, tt.forbiddenPart) {
				t.Errorf("output contains %q, truncation failed", tt.forbiddenPart)
			}

			hasIndicator := strings.Contains(res.Text, "(truncated)")
			if tt.wantTruncated != hasIndicator {
				t.Errorf("wantTruncated=%v, but hasIndicator=%v. Output: %q", tt.wantTruncated, hasIndicator, res.Text)
			}

			// Verify exact bytes for the "truncate middle of char" case
			if tt.name == "truncate middle of char" {
				// The output part before "Exit Code" and headers should be exactly "世"
				// Actually, ShellTool adds "Exit Code: 0\nOutput:\n"
				prefix := "Exit Code: 0\nOutput:\n"
				if !strings.HasPrefix(res.Text, prefix) {
					t.Fatalf("unexpected output format: %q", res.Text)
				}
				actualOutput := strings.TrimPrefix(res.Text, prefix)
				actualOutput = strings.TrimSuffix(actualOutput, "\n... (truncated)")

				if actualOutput != "世" {
					t.Errorf("expected exactly '世' (3 bytes), got %q (%d bytes)", actualOutput, len(actualOutput))
				}
			}
		})
	}
}
