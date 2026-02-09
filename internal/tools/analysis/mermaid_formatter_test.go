// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"strings"
	"testing"
)

func TestMermaidFormatter_Format(t *testing.T) {
	formatter := NewMermaidFormatter()

	tests := []struct {
		name     string
		frames   []CallFrame
		contains []string
	}{
		{
			name:   "Empty frames",
			frames: []CallFrame{},
			contains: []string{
				"sequenceDiagram",
			},
		},
		{
			name: "Happy path",
			frames: []CallFrame{
				{
					From:     "pkg_a",
					To:       "pkg_b",
					Function: "FuncB",
					Return:   "error",
				},
			},
			contains: []string{
				"sequenceDiagram",
				"participant pkg_a as pkg_a",
				"participant pkg_b as pkg_b",
				"pkg_a->>+pkg_b: FuncB",
				"pkg_b-->>-pkg_a: error",
			},
		},
		{
			name: "Single frame no return",
			frames: []CallFrame{
				{
					From:     "pkg_a",
					To:       "pkg_b",
					Function: "FuncB",
				},
			},
			contains: []string{
				"pkg_a->>+pkg_b: FuncB",
				"pkg_b-->>-pkg_a:  ", // Should contain a space if empty
			},
		},
		{
			name: "Recursive call",
			frames: []CallFrame{
				{
					From:     "pkg_a",
					To:       "pkg_a",
					Function: "Recursive",
				},
			},
			contains: []string{
				"participant pkg_a as pkg_a",
				"pkg_a->>+pkg_a: Recursive",
				"pkg_a-->>-pkg_a:  ",
			},
		},
		{
			name: "Async and Loop",
			frames: []CallFrame{
				{
					From:     "pkg_a",
					To:       "pkg_b",
					Function: "AsyncFunc",
					Async:    true,
					InLoop:   true,
				},
			},
			contains: []string{
				"loop for each",
				"pkg_a->>pkg_b: AsyncFunc (async)",
				"end",
			},
		},
		{
			name: "Complex scenario",
			frames: []CallFrame{
				{
					From:     "pkg_a",
					To:       "pkg_b",
					Function: "FuncB",
					Return:   "res",
				},
				{
					From:     "pkg_b",
					To:       "pkg_c",
					Function: "FuncC",
					Async:    true,
					InLoop:   true,
				},
			},
			contains: []string{
				"sequenceDiagram",
				"participant pkg_a as pkg_a",
				"participant pkg_b as pkg_b",
				"participant pkg_c as pkg_c",
				"pkg_a->>+pkg_b: FuncB",
				"pkg_b-->>-pkg_a: res",
				"loop for each",
				"pkg_b->>pkg_c: FuncC (async)",
				"end",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.Format(tt.frames)
			for _, part := range tt.contains {
				if !strings.Contains(result, part) {
					t.Errorf("Expected result to contain %q, but it didn't.\nGot:\n%s", part, result)
				}
			}
		})
	}
}
