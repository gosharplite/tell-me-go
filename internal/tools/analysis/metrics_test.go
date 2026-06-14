// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestCalculateSymbolComplexity_NilAndNonFunc(t *testing.T) {
	t.Parallel()

	analyzer := &defaultDeadCodeAnalyzer{}

	tests := []struct {
		name string
		obj  types.Object
		pkgs []*packages.Package
		want int
	}{
		{
			name: "nil object",
			obj:  nil,
			pkgs: nil,
			want: 0,
		},
		{
			name: "non-func object",
			obj:  types.NewVar(token.NoPos, nil, "MyVar", types.Typ[types.Int]),
			pkgs: nil,
			want: 0,
		},
		{
			name: "func object with no declaration found",
			obj: types.NewFunc(token.NoPos, nil, "FakeFunc",
				types.NewSignatureType(nil, nil, nil, nil, nil, false)),
			pkgs: []*packages.Package{},
			want: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := analyzer.calculateSymbolComplexity(tt.obj, tt.pkgs)
			if got != tt.want {
				t.Errorf("calculateSymbolComplexity() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCalculateImpactScore_NilAndNonFunc(t *testing.T) {
	t.Parallel()

	analyzer := &defaultDeadCodeAnalyzer{}

	tests := []struct {
		name string
		obj  types.Object
		want int
	}{
		{
			name: "nil object",
			obj:  nil,
			want: 0,
		},
		{
			name: "non-func object",
			obj:  types.NewVar(token.NoPos, nil, "MyVar", types.Typ[types.Int]),
			want: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := analyzer.calculateImpactScore(tt.obj, nil)
			if got != tt.want {
				t.Errorf("calculateImpactScore() = %d, want %d", got, tt.want)
			}
		})
	}
}
