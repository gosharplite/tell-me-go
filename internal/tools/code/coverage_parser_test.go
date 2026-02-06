// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package code

import (
	"strings"
	"testing"
)

func TestParseCoverageProfile(t *testing.T) {
	input := `mode: set
github.com/gosharplite/tell-me-go/internal/service/user.go:84.2,86.12 3 0
github.com/gosharplite/tell-me-go/internal/service/user.go:88.2,90.12 2 1
github.com/gosharplite/tell-me-go/internal/service/auth.go:10.5,12.10 4 0
`
	r := strings.NewReader(input)
	blocks, err := ParseCoverageProfile(r)
	if err != nil {
		t.Fatalf("ParseCoverageProfile failed: %v", err)
	}

	if len(blocks) != 2 {
		t.Errorf("expected 2 uncovered blocks, got %d", len(blocks))
	}

	expected := []UncoveredBlock{
		{File: "internal/service/user.go", Start: 84, End: 86, Stmts: 3},
		{File: "internal/service/auth.go", Start: 10, End: 12, Stmts: 4},
	}

	for i, b := range blocks {
		if b != expected[i] {
			t.Errorf("block %d: expected %+v, got %+v", i, expected[i], b)
		}
	}
}
