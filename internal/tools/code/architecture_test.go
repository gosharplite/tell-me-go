// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package code

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestCheckLayerViolations(t *testing.T) {
	t.Parallel()
	m := &ArchitectureManager{ModulePath: "github.com/org/repo"}

	tests := []struct {
		name          string
		pkgs          map[string][]string
		wantViolation bool
		wantReason    string
	}{
		{
			name: "Domain purity: Domain must not depend on Agent",
			pkgs: map[string][]string{
				"github.com/org/repo/internal/domain/user": {"github.com/org/repo/internal/agent/auth"},
			},
			wantViolation: true,
			wantReason:    "Domain must not depend on other internal layers.",
		},
		{
			name: "Domain purity: Domain can depend on Domain",
			pkgs: map[string][]string{
				"github.com/org/repo/internal/domain/user": {"github.com/org/repo/internal/domain/common"},
			},
			wantViolation: false,
		},
		{
			name: "Agent isolation: Agent must not depend on Tools",
			pkgs: map[string][]string{
				"github.com/org/repo/internal/agent/worker": {"github.com/org/repo/internal/tools/registry"},
			},
			wantViolation: true,
			wantReason:    "Application/Agent layer must not depend on Infrastructure/Tools implementations",
		},
		{
			name: "Infra isolation: Tools must not depend on Agent",
			pkgs: map[string][]string{
				"github.com/org/repo/internal/tools/registry": {"github.com/org/repo/internal/agent/worker"},
			},
			wantViolation: true,
			wantReason:    "Infrastructure layers must not depend on Application/Agent logic",
		},
		{
			name: "Cmd protection: internal package must not import cmd",
			pkgs: map[string][]string{
				"github.com/org/repo/internal/agent/worker": {"github.com/org/repo/cmd/app"},
			},
			wantViolation: true,
			wantReason:    "Composition Root (cmd)",
		},
		{
			name: "Cmd protection: Domain can't import cmd",
			pkgs: map[string][]string{
				"github.com/org/repo/internal/domain/user": {"github.com/org/repo/cmd/app"},
			},
			wantViolation: true,
			wantReason:    "Composition Root (cmd)",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			violations := m.checkLayerViolations(tt.pkgs)
			if tt.wantViolation {
				if len(violations) == 0 {
					t.Errorf("expected violations, got none")
				} else {
					found := false
					for _, v := range violations {
						if strings.Contains(v.reason, tt.wantReason) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected reason %q, got %q", tt.wantReason, violations[0].reason)
					}
				}
			} else {
				if len(violations) > 0 {
					t.Errorf("expected no violations, got %d: %v", len(violations), violations[0])
				}
			}
		})
	}
}

func TestCheckCircularDependencies(t *testing.T) {
	t.Parallel()
	m := &ArchitectureManager{ModulePath: "github.com/org/repo"}

	tests := []struct {
		name          string
		pkgs          map[string][]string
		wantViolation bool
	}{
		{
			name: "No cycles",
			pkgs: map[string][]string{
				"a": {"b"},
				"b": {"c"},
				"c": {},
			},
			wantViolation: false,
		},
		{
			name: "Simple cycle A -> B -> A",
			pkgs: map[string][]string{
				"a": {"b"},
				"b": {"a"},
			},
			wantViolation: true,
		},
		{
			name: "Longer cycle A -> B -> C -> A",
			pkgs: map[string][]string{
				"a": {"b"},
				"b": {"c"},
				"c": {"a"},
			},
			wantViolation: true,
		},
		{
			name: "Internal cycle not involving root",
			pkgs: map[string][]string{
				"root": {"a"},
				"a":    {"b"},
				"b":    {"c"},
				"c":    {"a"},
			},
			wantViolation: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			violations := m.checkCircularDependencies(tt.pkgs)
			if tt.wantViolation {
				if len(violations) == 0 {
					t.Errorf("expected circular dependency violations, got none")
				}
			} else {
				if len(violations) > 0 {
					t.Errorf("expected no violations, got %d", len(violations))
				}
			}
		})
	}
}

func TestVerifyArchitecture_Integration(t *testing.T) {
	// Smoke test against the live codebase. 
	// We don't assert failure/success strictly here because the codebase changes.
	// Instead, we verify it runs and returns a result.

	cwd, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			t.Skip("could not find go.mod, skipping integration test")
		}
		cwd = parent
	}
	oldCwd, _ := os.Getwd()
	os.Chdir(cwd)
	defer os.Chdir(oldCwd)

	sm := security.NewSecurityManager(nil)
	arc := &ArchitectureManager{SP: sm}

	ctx := context.Background()
	res, err := arc.VerifyArchitecture(ctx, nil)
	if err != nil {
		t.Fatalf("VerifyArchitecture failed: %v", err)
	}

	if res.Text == "" {
		t.Error("expected non-empty result text")
	}
}
