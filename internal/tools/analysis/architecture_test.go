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
	mockSP := &mockSecurityProvider{}
	m := &architectureManager{
		SP:         mockSP,
		ModulePath: "github.com/gosharplite/tell-me-go",
	}

	t.Run("violations found", func(t *testing.T) {
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
		res, err := m.VerifyArchitecture(context.Background(), nil)
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
		pkgs := map[string][]string{
			"github.com/gosharplite/tell-me-go/internal/domain": {},
		}
		m.Loader = &mockpackageProvider{pkgs: pkgs}
		res, err := m.VerifyArchitecture(context.Background(), nil)
		if err != nil {
			t.Fatalf("VerifyArchitecture failed: %v", err)
		}
		if !strings.Contains(res.Text, "integrity verified") {
			t.Error("expected success message")
		}
	})

	t.Run("load error", func(t *testing.T) {
		m.Loader = &mockpackageProvider{err: fmt.Errorf("load error")}
		_, err := m.VerifyArchitecture(context.Background(), nil)
		if err == nil {
			t.Error("expected error")
		}
	})
}

func TestArchitectureManager_FormatReport(t *testing.T) {
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
	tests := []struct {
		pkg   string
		layer string
		want  bool
	}{
		{"github.com/org/repo/internal/domain", "domain", true},
		{"github.com/org/repo/internal/domain/sub", "domain", true},
		{"github.com/org/repo/internal/domain-logic", "domain", false},
		{"github.com/org/repo/internal/agent/service", "agent", true},
		{"github.com/org/repo/pkg/domain", "domain", false},
	}

	for _, tt := range tests {
		t.Run(tt.pkg+"_"+tt.layer, func(t *testing.T) {
			if got := isLayer(tt.pkg, tt.layer); got != tt.want {
				t.Errorf("isLayer(%q, %q) = %v, want %v", tt.pkg, tt.layer, got, tt.want)
			}
		})
	}
}

func TestArchitectureManager_Shorten(t *testing.T) {
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
			if got := m.shorten(tt.pkg); got != tt.want {
				t.Errorf("shorten(%q) = %v, want %v", tt.pkg, got, tt.want)
			}
		})
	}
}

func TestArchitectureManager_CheckLayerViolations(t *testing.T) {
	m := &architectureManager{ModulePath: "github.com/org/repo"}
	pkgs := map[string][]string{
		"github.com/org/repo/internal/domain": {
			"github.com/org/repo/internal/agent", // Violation
		},
		"github.com/org/repo/internal/agent": {
			"github.com/org/repo/internal/domain", // OK
			"github.com/org/repo/cmd/app",         // Violation
		},
	}

	violations := m.checkLayerViolations(pkgs)
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
	m := &architectureManager{
		SP: &mockSecurityProviderDenyGo{},
	}
	executor := &mockExecutor{}
	r := &realpackageProvider{m: m, Exec: executor}

	t.Run("security denial", func(t *testing.T) {
		_, err := r.LoadPackages(context.Background())
		if err == nil || !strings.Contains(err.Error(), "security policy") {
			t.Errorf("expected security denial error, got %v", err)
		}
	})

	t.Run("command failure", func(t *testing.T) {
		m.SP = &mockSecurityProvider{} // allow go
		executor.CombinedOutputFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, fmt.Errorf("exit status 1")
		}
		_, err := r.LoadPackages(context.Background())
		if err == nil || !strings.Contains(err.Error(), "go list command failed") {
			t.Errorf("expected command failure error, got %v", err)
		}
	})

	t.Run("malformed output", func(t *testing.T) {
		executor.CombinedOutputFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("invalid json"), nil
		}
		_, err := r.LoadPackages(context.Background())
		if err == nil || !strings.Contains(err.Error(), "failed to decode go list output") {
			t.Errorf("expected decode error, got %v", err)
		}
	})

	t.Run("successful load", func(t *testing.T) {
		m.ModulePath = "github.com/org/repo"
		executor.CombinedOutputFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
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
