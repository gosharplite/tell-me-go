// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type mockpackageProvider struct {
	pkgs map[string][]string
	err  error
}

func (m *mockpackageProvider) LoadPackages(ctx context.Context) (map[string][]string, error) {
	return m.pkgs, m.err
}

func TestArchitectureManager_VerifyArchitecture(t *testing.T) {
	t.Parallel()

	t.Run("violations found", func(t *testing.T) {
		t.Parallel()
		mockSP := &mockSecurityProvider{}
		m := &architectureManager{
			SP:         mockSP,
			ModulePath: "github.com/gosharplite/tell-me-go",
		}

		pkgs := map[string][]string{
			"github.com/gosharplite/tell-me-go/internal/domain": {
				"github.com/gosharplite/tell-me-go/internal/agent", // Violation
			},
			"github.com/gosharplite/tell-me-go/internal/a": {
				"github.com/gosharplite/tell-me-go/internal/b",
			},
			"github.com/gosharplite/tell-me-go/internal/b": {
				"github.com/gosharplite/tell-me-go/internal/a", // Circular
			},
		}
		m.Loader = &mockpackageProvider{pkgs: pkgs}
		res, err := m.VerifyArchitecture(context.Background(), nil, nil)
		if err != nil {
			t.Fatalf("VerifyArchitecture failed: %v", err)
		}
		if !strings.Contains(res.Text, "[LAYER VIOLATION]") {
			t.Error("expected [LAYER VIOLATION]")
		}
		if !strings.Contains(res.Text, "[CIRCULAR REFERENCE]") {
			t.Error("expected [CIRCULAR REFERENCE]")
		}
	})

	t.Run("no violations", func(t *testing.T) {
		t.Parallel()
		mockSP := &mockSecurityProvider{}
		m := &architectureManager{
			SP:         mockSP,
			ModulePath: "github.com/gosharplite/tell-me-go",
		}

		pkgs := map[string][]string{
			"github.com/gosharplite/tell-me-go/internal/domain": {},
		}
		m.Loader = &mockpackageProvider{pkgs: pkgs}
		res, err := m.VerifyArchitecture(context.Background(), nil, nil)
		if err != nil {
			t.Fatalf("VerifyArchitecture failed: %v", err)
		}
		if !strings.Contains(res.Text, "integrity verified") {
			t.Error("expected success message")
		}
	})

	t.Run("load error", func(t *testing.T) {
		t.Parallel()
		mockSP := &mockSecurityProvider{}
		m := &architectureManager{
			SP:         mockSP,
			ModulePath: "github.com/gosharplite/tell-me-go",
		}

		m.Loader = &mockpackageProvider{err: fmt.Errorf("load error")}
		_, err := m.VerifyArchitecture(context.Background(), nil, nil)
		if err == nil {
			t.Error("expected error")
		}
	})
}

func TestArchitectureManager_FormatReport(t *testing.T) {
	t.Parallel()
	m := &architectureManager{
		ModulePath: "github.com/gosharplite/tell-me-go",
	}

	tests := []struct {
		name       string
		violations []violation
		contains   []string
	}{
		{
			name: "single violation",
			violations: []violation{
				{
					pkg:      "internal/domain",
					category: "[LAYER VIOLATION]",
					target:   "internal/agent",
					reason:   "Domain must not depend on other internal layers.",
				},
			},
			contains: []string{
				"### Architectural Integrity Report: ❌ FAILED",
				"| `internal/domain` | [LAYER VIOLATION] | `internal/agent` | Domain must not depend on other internal layers. |",
			},
		},
		{
			name: "multiple violations",
			violations: []violation{
				{
					pkg:      "internal/domain",
					category: "[LAYER VIOLATION]",
					target:   "internal/agent",
					reason:   "Reason 1",
				},
				{
					pkg:      "internal/agent",
					category: "[CIRCULAR REFERENCE]",
					target:   "internal/domain",
					reason:   "Cycle detected",
				},
			},
			contains: []string{
				"| `internal/domain` | [LAYER VIOLATION] | `internal/agent` | Reason 1 |",
				"| `internal/agent` | [CIRCULAR REFERENCE] | `internal/domain` | Cycle detected |",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := m.formatReport(tt.violations)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("formatReport() output does not contain %q\ngot: %s", want, got)
				}
			}
		})
	}
}

func TestArchitectureManager_IsLayer(t *testing.T) {
	t.Parallel()
	m := &architectureManager{ModulePath: "github.com/org/repo"}
	tests := []struct {
		pkg   string
		layer string
		want  bool
	}{
		{"github.com/org/repo/internal/domain", LayerDomain, true},
		{"github.com/org/repo/internal/domain/sub", LayerDomain, true},
		{"github.com/org/repo/internal/domain-logic", LayerDomain, false},
		{"github.com/org/repo/internal/agent/service", LayerApplication, true},
		{"github.com/org/repo/internal/pkg/stringsutil", LayerShared, true},
		{"github.com/org/repo/internal/infrastructure/toolchain/compiler", LayerInfrastructure, true},
		{"github.com/org/repo/pkg/domain", LayerDomain, false},
	}

	for _, tt := range tests {
		t.Run(tt.pkg+"_"+tt.layer, func(t *testing.T) {
			t.Parallel()
			if got := m.isLayer(tt.pkg, tt.layer); got != tt.want {
				t.Errorf("isLayer(%q, %q) = %v, want %v", tt.pkg, tt.layer, got, tt.want)
			}
		})
	}
}

func TestArchitectureManager_Shorten(t *testing.T) {
	t.Parallel()
	m := &architectureManager{ModulePath: "github.com/org/repo"}
	tests := []struct {
		pkg  string
		want string
	}{
		{"github.com/org/repo/internal/domain", "internal/domain"},
		{"github.com/org/repo/cmd/app", "cmd/app"},
		{"other/pkg", "other/pkg"},
	}

	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			t.Parallel()
			if got := m.shorten(tt.pkg); got != tt.want {
				t.Errorf("shorten(%q) = %v, want %v", tt.pkg, got, tt.want)
			}
		})
	}
}

func TestArchitectureManager_CheckLayerViolations(t *testing.T) {
	t.Parallel()
	m := &architectureManager{ModulePath: "github.com/org/repo"}
	pkgs := map[string][]string{
		"github.com/org/repo/internal/domain": {
			"github.com/org/repo/internal/agent",     // Violation
			"github.com/org/repo/internal/pkg/clock", // OK
		},
		"github.com/org/repo/internal/agent": {
			"github.com/org/repo/internal/domain",      // OK
			"github.com/org/repo/cmd/app",              // Violation
			"github.com/org/repo/internal/pkg/strings", // OK
		},
	}

	violations := m.checkLayerViolations(pkgs, nil)
	if len(violations) != 2 {
		t.Errorf("expected 2 violations, got %d", len(violations))
	}
}

type mockSecurityProviderDenyGo struct {
	mockSecurityProvider
}

func (m *mockSecurityProviderDenyGo) IsCommandAllowed(cmd string) bool {
	return cmd != "go"
}

func TestRealPackageProvider_LoadPackages(t *testing.T) {
	t.Parallel()

	t.Run("security denial", func(t *testing.T) {
		t.Parallel()
		m := &architectureManager{
			SP: &mockSecurityProviderDenyGo{},
		}
		runner := &mockAnalysisGoRunner{}
		r := &realpackageProvider{m: m, Runner: runner}
		_, err := r.LoadPackages(context.Background())
		if err == nil || !strings.Contains(err.Error(), "security policy") {
			t.Errorf("expected security denial error, got %v", err)
		}
	})

	t.Run("command failure", func(t *testing.T) {
		t.Parallel()
		m := &architectureManager{
			SP: &mockSecurityProvider{},
		}
		runner := &mockAnalysisGoRunner{}
		r := &realpackageProvider{m: m, Runner: runner}
		runner.getPackageListFunc = func(ctx context.Context, path string) ([]byte, error) {
			return nil, fmt.Errorf("exit status 1")
		}
		_, err := r.LoadPackages(context.Background())
		if err == nil || !strings.Contains(err.Error(), "go list command failed") {
			t.Errorf("expected command failure error, got %v", err)
		}
	})

	t.Run("malformed output", func(t *testing.T) {
		t.Parallel()
		m := &architectureManager{
			SP: &mockSecurityProvider{},
		}
		runner := &mockAnalysisGoRunner{}
		r := &realpackageProvider{m: m, Runner: runner}
		runner.getPackageListFunc = func(ctx context.Context, path string) ([]byte, error) {
			return []byte("invalid json"), nil
		}
		_, err := r.LoadPackages(context.Background())
		if err == nil || !strings.Contains(err.Error(), "failed to decode go list output") {
			t.Errorf("expected decode error, got %v", err)
		}
	})

	t.Run("successful load", func(t *testing.T) {
		t.Parallel()
		m := &architectureManager{
			SP:         &mockSecurityProvider{},
			ModulePath: "github.com/org/repo",
		}
		runner := &mockAnalysisGoRunner{}
		r := &realpackageProvider{m: m, Runner: runner}
		runner.getPackageListFunc = func(ctx context.Context, path string) ([]byte, error) {
			data := `{"ImportPath": "github.com/org/repo/internal/domain", "Imports": ["github.com/org/repo/internal/other"], "Module": {"Path": "github.com/org/repo"}}`
			return []byte(data), nil
		}
		pkgs, err := r.LoadPackages(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := pkgs["github.com/org/repo/internal/domain"]; !ok {
			t.Error("expected package not found")
		}
	})
}

func TestArchitectureManager_IsTrackedPackage(t *testing.T) {
	t.Parallel()
	m := &architectureManager{ModulePath: "github.com/org/repo"}

	tests := []struct {
		path string
		want bool
	}{
		{"github.com/org/repo/internal/domain", true},
		{"github.com/org/repo/cmd/app", true},
		{"github.com/org/repo/pkg/util", false},
		{"external.com/pkg", false},
	}

	for _, tt := range tests {
		if got := m.isTrackedPackage(tt.path); got != tt.want {
			t.Errorf("isTrackedPackage(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestArchitectureManager_Classify(t *testing.T) {
	t.Parallel()
	m := &architectureManager{ModulePath: "github.com/org/repo"}

	tests := []struct {
		pkg  string
		want string
	}{
		{"github.com/org/repo/internal/domain", LayerDomain},
		{"github.com/org/repo/internal/infrastructure/db", LayerInfrastructure},
		{"github.com/org/repo/internal/service/api", LayerApplication},
		{"github.com/org/repo/internal/tools/checker", LayerTools},
		{"github.com/org/repo/internal/pkg/utils", LayerShared},
		{"github.com/org/repo/cmd/server", LayerCmd},
		{"github.com/org/repo/internal/infrastructure/toolchain/compiler", LayerInfrastructure},
		// Edge Cases & Unknowns
		{"github.com/org/repo", LayerUnknown},                  // Module root
		{"github.com/org/repo/internal", LayerUnknown},         // Bare internal
		{"github.com/org/repo/internal/unknown", LayerUnknown}, // Unknown internal segment
		{"github.com/org/repo/pkg/external", LayerUnknown},     // Outside tracked directories
		{"github.com/other/module/pkg", LayerUnknown},          // External module
		{"", LayerUnknown}, // Empty path
	}

	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			if got := m.classify(tt.pkg); got != tt.want {
				t.Errorf("classify(%q) = %q, want %q", tt.pkg, got, tt.want)
			}
		})
	}
}
