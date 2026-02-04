// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"strings"
	"testing"
)

func TestMermaidFormatter_Format(t *testing.T) {
	formatter := NewMermaidFormatter()

	frames := []CallFrame{
		{
			From:     "pkg_a",
			To:       "pkg_b",
			Function: "FuncB",
			Async:    false,
			InLoop:   false,
			Return:   "error",
		},
		{
			From:     "pkg_b",
			To:       "pkg_c",
			Function: "FuncC",
			Async:    true,
			InLoop:   true,
			Return:   "",
		},
	}

	result := formatter.Format(frames)

	expectedParts := []string{
		"sequenceDiagram",
		"participant pkg_a as pkg_a",
		"participant pkg_b as pkg_b",
		"participant pkg_c as pkg_c",
		"pkg_a->>+pkg_b: FuncB",
		"pkg_b-->>-pkg_a: error",
		"loop for each",
		"pkg_b->>pkg_c: FuncC (async)",
		"end",
	}

	for _, part := range expectedParts {
		if !strings.Contains(result, part) {
			t.Errorf("Expected result to contain %q, but it didn't.\nGot:\n%s", part, result)
		}
	}
}
