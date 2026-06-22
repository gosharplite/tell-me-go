// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestNolintGateEvaluator(t *testing.T) {
	tests := []struct {
		name    string
		ctx     *orphanEvalContext
		wantNil bool // true if evaluate should return nil (exclude symbol)
	}{
		{
			name:    "nil context",
			ctx:     nil,
			wantNil: true,
		},
		{
			name: "nil meta sub-field",
			ctx: &orphanEvalContext{
				id:    "example.com/pkg.Symbol",
				meta:  nil,
				state: &scanState{},
			},
			wantNil: false,
		},
		{
			name: "nil state sub-field",
			ctx: &orphanEvalContext{
				id:    "example.com/pkg.Symbol",
				meta:  &symMeta{obj: nil},
				state: nil,
			},
			wantNil: false,
		},
		{
			name: "nil meta.obj sub-field",
			ctx: &orphanEvalContext{
				id: "example.com/pkg.Symbol",
				meta: &symMeta{
					name:    "Symbol",
					pkgPath: "example.com/pkg",
					symType: "Function",
					obj:     nil,
				},
				state: &scanState{
					pkgs: make([]*packages.Package, 0),
				},
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &nolintGateEvaluator{}
			got := e.evaluate(tt.ctx)

			if tt.wantNil && got != nil {
				t.Errorf("expected nil result, got %v", got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("expected non-nil result (context pass-through), got nil")
			}
		})
	}
}

func TestTextMatchWarningEvaluator(t *testing.T) {
	validReport := &OrphanReport{
		Symbol:   "Symbol",
		Pkg:      "example.com/pkg",
		Type:     "Function",
		Severity: "DEAD",
		Reason:   "No references found within the module.",
	}

	tests := []struct {
		name    string
		e       *textMatchWarningEvaluator
		ctx     *orphanEvalContext
		wantNil bool
	}{
		{
			name:    "nil context",
			e:       &textMatchWarningEvaluator{},
			ctx:     nil,
			wantNil: true, // nil ctx in → nil ctx out
		},
		{
			name: "nil report",
			e:    &textMatchWarningEvaluator{},
			ctx: &orphanEvalContext{
				id:     "example.com/pkg.Symbol",
				meta:   &symMeta{},
				state:  &scanState{},
				report: nil,
			},
			wantNil: false, // returns ctx (non-nil)
		},
		{
			name: "nil meta",
			e:    &textMatchWarningEvaluator{},
			ctx: &orphanEvalContext{
				id:     "example.com/pkg.Symbol",
				meta:   nil,
				state:  &scanState{},
				report: validReport,
			},
			wantNil: false, // returns ctx (non-nil)
		},
		{
			name: "nil state",
			e:    &textMatchWarningEvaluator{},
			ctx: &orphanEvalContext{
				id:     "example.com/pkg.Symbol",
				meta:   &symMeta{},
				state:  nil,
				report: validReport,
			},
			wantNil: false, // returns ctx (non-nil)
		},
		{
			name: "nil analyzer",
			e:    &textMatchWarningEvaluator{analyzer: nil},
			ctx: &orphanEvalContext{
				id: "example.com/pkg.Symbol",
				meta: &symMeta{
					name:     "Symbol",
					pkgPath:  "example.com/pkg",
					symType:  "Function",
					isMethod: false,
				},
				state:  &scanState{},
				deep:   false,
				report: validReport,
			},
			wantNil: false, // returns ctx (non-nil)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.e.evaluate(tt.ctx)
			if tt.wantNil && got != nil {
				t.Errorf("expected nil result, got %v", got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("expected non-nil result (context pass-through), got nil")
			}
		})
	}
}

func TestDeepVerificationEvaluator(t *testing.T) {
	validReport := &OrphanReport{
		Symbol:   "Symbol",
		Pkg:      "example.com/pkg",
		Type:     "Method",
		Severity: "DEAD",
		Reason:   "No references found within the module.",
	}

	tests := []struct {
		name    string
		e       *deepVerificationEvaluator
		ctx     *orphanEvalContext
		wantNil bool
	}{
		{
			name:    "nil context",
			e:       &deepVerificationEvaluator{},
			ctx:     nil,
			wantNil: true,
		},
		{
			name: "nil report",
			e:    &deepVerificationEvaluator{},
			ctx: &orphanEvalContext{
				id:     "example.com/pkg.Symbol",
				meta:   &symMeta{isMethod: true},
				state:  &scanState{},
				deep:   true,
				report: nil,
			},
			wantNil: false,
		},
		{
			name: "nil meta",
			e:    &deepVerificationEvaluator{},
			ctx: &orphanEvalContext{
				id:     "example.com/pkg.Symbol",
				meta:   nil,
				state:  &scanState{},
				deep:   true,
				report: validReport,
			},
			wantNil: false,
		},
		{
			name: "nil state",
			e:    &deepVerificationEvaluator{},
			ctx: &orphanEvalContext{
				id:     "example.com/pkg.Symbol",
				meta:   &symMeta{isMethod: true},
				state:  nil,
				deep:   true,
				report: validReport,
			},
			wantNil: false,
		},
		{
			name: "nil analyzer",
			e:    &deepVerificationEvaluator{analyzer: nil},
			ctx: &orphanEvalContext{
				id: "example.com/pkg.Symbol",
				meta: &symMeta{
					name:     "Symbol",
					pkgPath:  "example.com/pkg",
					symType:  "Method",
					isMethod: true,
				},
				state:  &scanState{},
				deep:   true,
				report: validReport,
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.e.evaluate(tt.ctx)
			if tt.wantNil && got != nil {
				t.Errorf("expected nil result, got %v", got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("expected non-nil result (context pass-through), got nil")
			}
		})
	}
}
func TestDeepVerificationEvaluator_ShouldDeepVerify(t *testing.T) {
	tests := []struct {
		name string
		e    *deepVerificationEvaluator
		ctx  *orphanEvalContext
		want bool
	}{
		{
			name: "nil context",
			e:    &deepVerificationEvaluator{analyzer: &defaultDeadCodeAnalyzer{}},
			ctx:  nil,
			want: false,
		},
		{
			name: "nil report",
			e:    &deepVerificationEvaluator{analyzer: &defaultDeadCodeAnalyzer{}},
			ctx: &orphanEvalContext{
				report: nil,
				meta:   &symMeta{isMethod: true},
				state:  &scanState{},
				deep:   true,
			},
			want: false,
		},
		{
			name: "nil meta",
			e:    &deepVerificationEvaluator{analyzer: &defaultDeadCodeAnalyzer{}},
			ctx: &orphanEvalContext{
				report: &OrphanReport{},
				meta:   nil,
				state:  &scanState{},
				deep:   true,
			},
			want: false,
		},
		{
			name: "nil state",
			e:    &deepVerificationEvaluator{analyzer: &defaultDeadCodeAnalyzer{}},
			ctx: &orphanEvalContext{
				report: &OrphanReport{},
				meta:   &symMeta{isMethod: true},
				state:  nil,
				deep:   true,
			},
			want: false,
		},
		{
			name: "nil analyzer",
			e:    &deepVerificationEvaluator{analyzer: nil},
			ctx: &orphanEvalContext{
				report: &OrphanReport{},
				meta:   &symMeta{isMethod: true},
				state:  &scanState{},
				deep:   true,
			},
			want: false,
		},
		{
			name: "deep disabled",
			e:    &deepVerificationEvaluator{analyzer: &defaultDeadCodeAnalyzer{}},
			ctx: &orphanEvalContext{
				report: &OrphanReport{},
				meta:   &symMeta{isMethod: true},
				state:  &scanState{},
				deep:   false,
			},
			want: false,
		},
		{
			name: "not a method",
			e:    &deepVerificationEvaluator{analyzer: &defaultDeadCodeAnalyzer{}},
			ctx: &orphanEvalContext{
				report: &OrphanReport{},
				meta:   &symMeta{isMethod: false},
				state:  &scanState{},
				deep:   true,
			},
			want: false,
		},
		{
			name: "all conditions met",
			e:    &deepVerificationEvaluator{analyzer: &defaultDeadCodeAnalyzer{}},
			ctx: &orphanEvalContext{
				report: &OrphanReport{},
				meta:   &symMeta{isMethod: true},
				state:  &scanState{},
				deep:   true,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.e.shouldDeepVerify(tt.ctx)
			if got != tt.want {
				t.Errorf("shouldDeepVerify() = %v, want %v", got, tt.want)
			}
		})
	}
}
func TestAnonInterfaceWarningEvaluator(t *testing.T) {
	validReport := &OrphanReport{
		Symbol:   "Symbol",
		Pkg:      "example.com/pkg",
		Type:     "Method",
		Severity: "DEAD",
		Reason:   "No references found within the module.",
	}

	tests := []struct {
		name    string
		e       *anonInterfaceWarningEvaluator
		ctx     *orphanEvalContext
		wantNil bool
	}{
		{
			name:    "nil context",
			e:       &anonInterfaceWarningEvaluator{},
			ctx:     nil,
			wantNil: true,
		},
		{
			name: "nil report",
			e:    &anonInterfaceWarningEvaluator{},
			ctx: &orphanEvalContext{
				id:     "example.com/pkg.Symbol",
				meta:   &symMeta{isMethod: true},
				state:  &scanState{},
				report: nil,
			},
			wantNil: false,
		},
		{
			name: "nil meta",
			e:    &anonInterfaceWarningEvaluator{},
			ctx: &orphanEvalContext{
				id:     "example.com/pkg.Symbol",
				meta:   nil,
				state:  &scanState{},
				report: validReport,
			},
			wantNil: false,
		},
		{
			name: "nil state",
			e:    &anonInterfaceWarningEvaluator{},
			ctx: &orphanEvalContext{
				id:     "example.com/pkg.Symbol",
				meta:   &symMeta{isMethod: true},
				state:  nil,
				report: validReport,
			},
			wantNil: false,
		},
		{
			name: "nil analyzer",
			e:    &anonInterfaceWarningEvaluator{analyzer: nil},
			ctx: &orphanEvalContext{
				id: "example.com/pkg.Symbol",
				meta: &symMeta{
					name:     "Symbol",
					pkgPath:  "example.com/pkg",
					symType:  "Method",
					isMethod: true,
				},
				state:  &scanState{},
				report: validReport,
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.e.evaluate(tt.ctx)
			if tt.wantNil && got != nil {
				t.Errorf("expected nil result, got %v", got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("expected non-nil result (context pass-through), got nil")
			}
		})
	}
}

func TestUsageGateEvaluator(t *testing.T) {
	tests := []struct {
		name    string
		ctx     *orphanEvalContext
		wantNil bool
	}{
		{
			name:    "nil context",
			ctx:     nil,
			wantNil: true,
		},
		{
			name: "nil state",
			ctx: &orphanEvalContext{
				id:    "example.com/pkg.Symbol",
				meta:  &symMeta{},
				state: nil,
			},
			wantNil: false,
		},
		{
			name: "nil totalUses and externalUses maps",
			ctx: &orphanEvalContext{
				id:   "example.com/pkg.Symbol",
				meta: &symMeta{},
				state: &scanState{
					totalUses:    nil,
					externalUses: nil,
				},
			},
			wantNil: false,
		},
		{
			name: "missing id in maps (zero values)",
			ctx: &orphanEvalContext{
				id:   "example.com/pkg.Symbol",
				meta: &symMeta{},
				state: &scanState{
					totalUses:    map[string]int{"other.symbol": 1},
					externalUses: map[string]int{"other.symbol": 1},
				},
			},
			wantNil: false,
		},
		{
			name: "both total and external > 0 excludes symbol",
			ctx: &orphanEvalContext{
				id:   "example.com/pkg.Symbol",
				meta: &symMeta{},
				state: &scanState{
					totalUses:    map[string]int{"example.com/pkg.Symbol": 1},
					externalUses: map[string]int{"example.com/pkg.Symbol": 1},
				},
			},
			wantNil: true,
		},
		{
			name: "total > 0 but external == 0 does not exclude",
			ctx: &orphanEvalContext{
				id:   "example.com/pkg.Symbol",
				meta: &symMeta{},
				state: &scanState{
					totalUses:    map[string]int{"example.com/pkg.Symbol": 1},
					externalUses: map[string]int{"example.com/pkg.Symbol": 0},
				},
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &usageGateEvaluator{}
			got := e.evaluate(tt.ctx)
			if tt.wantNil && got != nil {
				t.Errorf("expected nil result, got %v", got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("expected non-nil result (context pass-through), got nil")
			}
		})
	}
}

func TestSeverityClassifierEvaluator(t *testing.T) {
	tests := []struct {
		name         string
		ctx          *orphanEvalContext
		wantNil      bool
		wantSeverity string // only checked when wantNil is false
	}{
		{
			name:    "nil context",
			ctx:     nil,
			wantNil: true,
		},
		{
			name: "nil state",
			ctx: &orphanEvalContext{
				id:    "example.com/pkg.Symbol",
				meta:  &symMeta{},
				state: nil,
			},
			wantNil: true,
		},
		{
			name: "nil meta",
			ctx: &orphanEvalContext{
				id:    "example.com/pkg.Symbol",
				meta:  nil,
				state: &scanState{},
			},
			wantNil: true,
		},
		{
			name: "nil totalUses map returns DEAD",
			ctx: &orphanEvalContext{
				id:          "example.com/pkg.Symbol",
				displayName: "Symbol",
				meta: &symMeta{
					pkgPath: "example.com/pkg",
					symType: "Function",
				},
				state: &scanState{
					totalUses:    nil,
					externalUses: nil,
				},
			},
			wantNil:      false,
			wantSeverity: "DEAD",
		},
		{
			name: "missing id in map returns DEAD",
			ctx: &orphanEvalContext{
				id:          "example.com/pkg.Symbol",
				displayName: "Symbol",
				meta: &symMeta{
					pkgPath: "example.com/pkg",
					symType: "Function",
				},
				state: &scanState{
					totalUses:    map[string]int{"other.symbol": 5},
					externalUses: map[string]int{},
				},
			},
			wantNil:      false,
			wantSeverity: "DEAD",
		},
		{
			name: "zero total uses classified DEAD",
			ctx: &orphanEvalContext{
				id:          "example.com/pkg.Symbol",
				displayName: "Symbol",
				meta: &symMeta{
					pkgPath: "example.com/pkg",
					symType: "Function",
				},
				state: &scanState{
					totalUses:    map[string]int{"example.com/pkg.Symbol": 0},
					externalUses: map[string]int{},
				},
				complexity: 5,
				impact:     2,
			},
			wantNil:      false,
			wantSeverity: "DEAD",
		},
		{
			name: "positive total uses classified PRIVATE",
			ctx: &orphanEvalContext{
				id:          "example.com/pkg.Symbol",
				displayName: "Symbol",
				meta: &symMeta{
					pkgPath: "example.com/pkg",
					symType: "Function",
				},
				state: &scanState{
					totalUses:    map[string]int{"example.com/pkg.Symbol": 3},
					externalUses: map[string]int{},
				},
				complexity: 8,
				impact:     1,
			},
			wantNil:      false,
			wantSeverity: "PRIVATE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &severityClassifierEvaluator{}
			got := e.evaluate(tt.ctx)

			if tt.wantNil && got != nil {
				t.Errorf("expected nil result, got %v", got)
			}
			if !tt.wantNil {
				if got == nil {
					t.Errorf("expected non-nil result, got nil")
					return
				}
				if got.report == nil {
					t.Errorf("expected ctx.report to be populated, got nil")
					return
				}
				if got.report.Severity != tt.wantSeverity {
					t.Errorf("expected severity %q, got %q", tt.wantSeverity, got.report.Severity)
				}
			}
		})
	}
}
func TestComplexityReclassifierEvaluator(t *testing.T) {
	tests := []struct {
		name       string
		ctx        *orphanEvalContext
		wantNil    bool
		wantReason string
	}{
		{
			name:    "nil context",
			ctx:     nil,
			wantNil: true,
		},
		{
			name: "nil report",
			ctx: &orphanEvalContext{
				id:     "example.com/pkg.Symbol",
				report: nil,
			},
			wantNil: false,
		},
		{
			name: "PRIVATE with low complexity below threshold",
			ctx: &orphanEvalContext{
				id:         "example.com/pkg.Symbol",
				complexity: 5,
				report: &OrphanReport{
					Symbol:   "Symbol",
					Pkg:      "example.com/pkg",
					Type:     "Function",
					Severity: "PRIVATE",
					Reason:   "Exported symbol is only used within its own package.",
				},
			},
			wantNil:    false,
			wantReason: "Exported symbol is only used within its own package.",
		},
		{
			name: "PRIVATE with complexity exactly at threshold",
			ctx: &orphanEvalContext{
				id:         "example.com/pkg.Symbol",
				complexity: 10,
				report: &OrphanReport{
					Symbol:   "Symbol",
					Pkg:      "example.com/pkg",
					Type:     "Function",
					Severity: "PRIVATE",
					Reason:   "Exported symbol is only used within its own package.",
				},
			},
			wantNil:    false,
			wantReason: "High Priority Refactoring Candidate: can be refactored with zero external impact.",
		},
		{
			name: "DEAD severity not reclassified regardless of complexity",
			ctx: &orphanEvalContext{
				id:         "example.com/pkg.Symbol",
				complexity: 15,
				report: &OrphanReport{
					Symbol:   "Symbol",
					Pkg:      "example.com/pkg",
					Type:     "Function",
					Severity: "DEAD",
					Reason:   "No references found within the module.",
				},
			},
			wantNil:    false,
			wantReason: "No references found within the module.",
		},
		{
			name: "PRIVATE with complexity above threshold",
			ctx: &orphanEvalContext{
				id:         "example.com/pkg.Symbol",
				complexity: 25,
				report: &OrphanReport{
					Symbol:   "Symbol",
					Pkg:      "example.com/pkg",
					Type:     "Function",
					Severity: "PRIVATE",
					Reason:   "Exported symbol is only used within its own package.",
				},
			},
			wantNil:    false,
			wantReason: "High Priority Refactoring Candidate: can be refactored with zero external impact.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &complexityReclassifierEvaluator{}
			got := e.evaluate(tt.ctx)

			if tt.wantNil && got != nil {
				t.Errorf("expected nil result, got %v", got)
			}
			if !tt.wantNil {
				if got == nil {
					t.Errorf("expected non-nil result, got nil")
					return
				}
				if got.report == nil {
					if tt.ctx != nil && tt.ctx.report == nil {
						return
					}
					t.Errorf("expected ctx.report to be non-nil, got nil")
					return
				}
				if got.report.Reason != tt.wantReason {
					t.Errorf("expected Reason %q, got %q", tt.wantReason, got.report.Reason)
				}
			}
		})
	}
}
