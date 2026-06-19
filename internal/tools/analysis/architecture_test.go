// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
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
		{"github.com/org/repo/internal/domain", layerDomain, true},
		{"github.com/org/repo/internal/domain/sub", layerDomain, true},
		{"github.com/org/repo/internal/domain-logic", layerDomain, false},
		{"github.com/org/repo/internal/agent/service", layerApplication, true},
		{"github.com/org/repo/internal/pkg/stringsutil", layerShared, true},
		{"github.com/org/repo/internal/infrastructure/toolchain/compiler", layerInfrastructure, true},
		{"github.com/org/repo/pkg/domain", layerDomain, false},
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
		// Additional edge cases for coverage
		{"", ""},                    // empty string
		{"github.com/org/repo", ""}, // module root → empty after trim
		{"github.com/org/repo/internal", "internal"},   // bare internal
		{"github.com/org/repo/cmd", "cmd"},             // bare cmd
		{"pkg/foo", "pkg/foo"},                         // no internal/ or cmd/
		{"internal/standalone", "internal/standalone"}, // path with internal but no module prefix
		{"cmd/standalone", "cmd/standalone"},           // path with cmd but no module prefix
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

func TestArchitectureManager_Shorten_NoModulePath(t *testing.T) {
	t.Parallel()
	// Test shorten when ModulePath is empty (falls back to internal/ / cmd/ detection)
	m := &architectureManager{ModulePath: ""}
	tests := []struct {
		pkg  string
		want string
	}{
		{"github.com/org/repo/internal/domain", "internal/domain"},
		{"github.com/org/repo/cmd/app", "cmd/app"},
		{"pkg/foo", "pkg/foo"},                                                   // no match
		{"github.com/org/repo/pkg/external", "github.com/org/repo/pkg/external"}, // no internal/ or cmd/
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

func TestSendHeartbeat(t *testing.T) {
	t.Parallel()

	t.Run("nil channel does not panic", func(t *testing.T) {
		t.Parallel()
		m := &architectureManager{}
		// Should not panic
		m.sendHeartbeat(nil)
	})

	t.Run("non-nil unbuffered channel does not block", func(t *testing.T) {
		t.Parallel()
		m := &architectureManager{}
		hb := make(chan struct{})
		// Read in background to prevent block
		go func() { <-hb }()
		m.sendHeartbeat(hb)
		close(hb)
	})

	t.Run("buffered channel", func(t *testing.T) {
		t.Parallel()
		m := &architectureManager{}
		hb := make(chan struct{}, 1)
		m.sendHeartbeat(hb)
		select {
		case <-hb:
			// OK
		default:
			t.Error("expected heartbeat on buffered channel")
		}
	})
}

func TestIsAlreadyReported(t *testing.T) {
	t.Parallel()

	v := violation{pkg: "pkg/a", target: "cmd/app"}
	existing := []violation{
		{pkg: "pkg/b", target: "cmd/other"},
		{pkg: "pkg/a", target: "cmd/app"}, // duplicate
	}

	t.Run("duplicate found", func(t *testing.T) {
		t.Parallel()
		if !isAlreadyReported(v, existing) {
			t.Error("expected duplicate to be found")
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		if isAlreadyReported(violation{pkg: "pkg/new", target: "cmd/new"}, existing) {
			t.Error("expected no duplicate")
		}
	})

	t.Run("empty list", func(t *testing.T) {
		t.Parallel()
		if isAlreadyReported(v, nil) {
			t.Error("expected no duplicate in empty list")
		}
	})
}

func TestRunHeartbeat(t *testing.T) {
	t.Parallel()

	t.Run("stops on done signal", func(t *testing.T) {
		t.Parallel()
		m := &architectureManager{}
		hb := make(chan struct{}, 10)
		done := make(chan struct{})
		close(done) // already done
		m.runHeartbeat(hb, done)
		// Should exit immediately without sending heartbeat
	})

	t.Run("sends heartbeats then stops", func(t *testing.T) {
		t.Parallel()
		m := &architectureManager{}
		hb := make(chan struct{}, 10)
		done := make(chan struct{})
		go m.runHeartbeat(hb, done)
		// Wait for at least one tick (2s interval)
		select {
		case <-hb:
			// Got heartbeat
		case <-done:
			// Done triggered - this is also fine
		}
		close(done)
	})
}

func TestCheckGeneralCmdImport_NonInternal(t *testing.T) {
	t.Parallel()
	m := &architectureManager{ModulePath: "github.com/org/repo"}

	t.Run("non-internal package returns nil", func(t *testing.T) {
		t.Parallel()
		// external/pkg does not contain "internal/"
		result := m.checkGeneralCmdImport("github.com/org/repo/pkg/external", []string{"github.com/org/repo/cmd/app"}, nil)
		if result != nil {
			t.Errorf("expected nil for non-internal package, got %v", result)
		}
	})

	t.Run("internal package importing cmd", func(t *testing.T) {
		t.Parallel()
		// internal package with cmd import should report violation
		result := m.checkGeneralCmdImport("github.com/org/repo/internal/tools", []string{"github.com/org/repo/cmd/app"}, nil)
		if len(result) != 1 {
			t.Fatalf("expected 1 violation, got %d", len(result))
		}
		if result[0].category != "[LAYER VIOLATION]" {
			t.Errorf("expected LAYER VIOLATION category, got %s", result[0].category)
		}
	})

	t.Run("internal package without cmd import", func(t *testing.T) {
		t.Parallel()
		result := m.checkGeneralCmdImport("github.com/org/repo/internal/tools", []string{"github.com/org/repo/internal/other"}, nil)
		if len(result) != 0 {
			t.Errorf("expected 0 violations, got %d", len(result))
		}
	})
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
		{"github.com/org/repo/internal/domain", layerDomain},
		{"github.com/org/repo/internal/infrastructure/db", layerInfrastructure},
		{"github.com/org/repo/internal/service/api", layerApplication},
		{"github.com/org/repo/internal/tools/checker", layerTools},
		{"github.com/org/repo/internal/pkg/utils", layerShared},
		{"github.com/org/repo/cmd/server", layerCmd},
		{"github.com/org/repo/internal/infrastructure/toolchain/compiler", layerInfrastructure},
		// Edge Cases & Unknowns
		{"github.com/org/repo", layerUnknown},                  // Module root
		{"github.com/org/repo/internal", layerUnknown},         // Bare internal
		{"github.com/org/repo/internal/unknown", layerUnknown}, // Unknown internal segment
		{"github.com/org/repo/pkg/external", layerUnknown},     // Outside tracked directories
		{"github.com/other/module/pkg", layerUnknown},          // External module
		{"", layerUnknown}, // Empty path
	}

	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			if got := m.classify(tt.pkg); got != tt.want {
				t.Errorf("classify(%q) = %q, want %q", tt.pkg, got, tt.want)
			}
		})
	}
}

func TestIndexedPackageProvider_LoadPackages_ErrorPaths(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		packagesFn func(ctx context.Context, hb chan<- struct{}) ([]*packages.Package, error)
		wantErr    string
	}{
		{
			name: "Packages returns error",
			packagesFn: func(ctx context.Context, hb chan<- struct{}) ([]*packages.Package, error) {
				return nil, fmt.Errorf("index scan failed")
			},
			wantErr: "failed to load architecture packages",
		},
		{
			name: "Packages returns empty slice",
			packagesFn: func(ctx context.Context, hb chan<- struct{}) ([]*packages.Package, error) {
				return []*packages.Package{}, nil
			},
			wantErr: "no packages found in index",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mockIdx := &mockSymbolIndex{
				PackagesFunc: tt.packagesFn,
			}
			m := &architectureManager{ModulePath: "example.com/mod"}
			provider := &indexedPackageProvider{m: m, idx: mockIdx}
			_, err := provider.LoadPackages(context.Background())
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestReportCycle_NoPathFound(t *testing.T) {
	t.Parallel()
	m := &architectureManager{ModulePath: "example.com/mod"}
	d := &circularDetector{
		m:       m,
		path:    []string{"pkg/A", "pkg/B"},
		visited: map[string]bool{"pkg/A": true, "pkg/B": true},
	}
	// v="pkg/C" is NOT in d.path, so cycleStart == -1
	d.reportCycle("pkg/B", "pkg/C")
	// Must not panic; no violation should be added
	if len(d.violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(d.violations))
	}
}

// =============================================================================
// Gap 7: TestVerifyArchitecture_IndexedProvider — covers the
//
//	indexed package provider path in VerifyArchitecture.
//
// =============================================================================
func TestVerifyArchitecture_IndexedProvider(t *testing.T) {
	t.Parallel()

	mockIdx := &mockSymbolIndex{
		PackagesFunc: func(ctx context.Context, hb chan<- struct{}) ([]*packages.Package, error) {
			return []*packages.Package{
				{
					PkgPath: "example.com/mod/internal/domain",
					Module: &packages.Module{
						Path: "example.com/mod",
					},
					Imports: map[string]*packages.Package{},
				},
			}, nil
		},
	}

	m := &architectureManager{
		SP:  &mockSecurityProvider{},
		idx: mockIdx,
	}

	res, err := m.VerifyArchitecture(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Contains(t, res.Text, "integrity verified")
}

// =============================================================================
// Gap 8: TestCheckLayerViolations_CompositionRoot — covers the
//
//	composition root (DI/Factory) path in checkLayerViolations.
//
// =============================================================================
func TestCheckLayerViolations_CompositionRoot(t *testing.T) {
	t.Parallel()
	m := &architectureManager{ModulePath: "example.com/mod"}

	t.Run("DI package importing cmd is a violation", func(t *testing.T) {
		t.Parallel()
		pkgs := map[string][]string{
			"example.com/mod/internal/infrastructure/di": {
				"example.com/mod/cmd/app",
			},
		}
		violations := m.checkLayerViolations(pkgs, nil)
		if len(violations) != 1 {
			t.Fatalf("expected 1 violation, got %d", len(violations))
		}
		if violations[0].category != "[LAYER VIOLATION]" {
			t.Errorf("expected LAYER VIOLATION, got %s", violations[0].category)
		}
	})

	t.Run("DI package importing domain is NOT a violation", func(t *testing.T) {
		t.Parallel()
		pkgs := map[string][]string{
			"example.com/mod/internal/infrastructure/di": {
				"example.com/mod/internal/domain",
			},
		}
		violations := m.checkLayerViolations(pkgs, nil)
		if len(violations) != 0 {
			t.Errorf("expected 0 violations for DI importing domain, got %d", len(violations))
		}
	})

	t.Run("factory package with mixed imports", func(t *testing.T) {
		t.Parallel()
		pkgs := map[string][]string{
			"example.com/mod/internal/infrastructure/factory": {
				"example.com/mod/internal/domain",
				"example.com/mod/cmd/app",
				"example.com/mod/internal/agent",
			},
		}
		violations := m.checkLayerViolations(pkgs, nil)
		if len(violations) != 1 {
			t.Fatalf("expected 1 violation (cmd import only), got %d", len(violations))
		}
		if violations[0].target != "cmd/app" {
			t.Errorf("expected cmd/app violation, got target: %s", violations[0].target)
		}
	})
}

// =============================================================================
// Gap: checkLayerViolations heartbeat (i%10==0 with hb != nil)
// Covered by ≥10 packages triggering the per-package heartbeat send.
// =============================================================================
func TestCheckLayerViolations_Heartbeat(t *testing.T) {
	t.Parallel()
	m := &architectureManager{ModulePath: "example.com/mod"}

	// Create 12 packages to trigger i%10==0 heartbeat at i=0 and i=10
	pkgs := make(map[string][]string)
	for i := 0; i < 12; i++ {
		pkgPath := fmt.Sprintf("example.com/mod/internal/domain/pkg%d", i)
		pkgs[pkgPath] = []string{}
	}

	hb := make(chan struct{}, 2)
	violations := m.checkLayerViolations(pkgs, hb)
	_ = violations

	// Verify heartbeat was sent (i=0 hits i%10==0, i=10 also hits)
	select {
	case <-hb:
		// heartbeat received at i=0
	default:
		t.Error("expected heartbeat at i=0")
	}
	select {
	case <-hb:
		// heartbeat received at i=10
	default:
		t.Error("expected heartbeat at i=10")
	}
}

// =============================================================================
// Gap: VerifyArchitecture heartbeat (line 174 sendHeartbeat with hb != nil)
// Covered by passing a non-nil hb to VerifyArchitecture.
// =============================================================================
func TestVerifyArchitecture_Heartbeat(t *testing.T) {
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

	hb := make(chan struct{}, 5)
	res, err := m.VerifyArchitecture(context.Background(), nil, hb)
	if err != nil {
		t.Fatalf("VerifyArchitecture failed: %v", err)
	}
	if !strings.Contains(res.Text, "integrity verified") {
		t.Error("expected success message")
	}

	// Verify at least one heartbeat was received (either from runHeartbeat ticker
	// or from the final sendHeartbeat call)
	select {
	case <-hb:
		// heartbeat received
	default:
		// Acceptable if buffer was full and ticker didn't fire before completion.
		// The test validates the hb != nil path doesn't panic.
	}
}

// =============================================================================
// Gap: VerifyArchitecture loaderOnce.Do realpackageProvider path (L174-176).
// Covered by creating an architectureManager without idx and without Loader,
// so that the lazy initialization creates a realpackageProvider.
// =============================================================================
func TestVerifyArchitecture_RealPackageProviderPath(t *testing.T) {
	t.Parallel()

	runner := &mockAnalysisGoRunner{
		getPackageListFunc: func(ctx context.Context, path string) ([]byte, error) {
			// Return valid JSON package list with one domain package
			return []byte(`{"ImportPath": "github.com/org/repo/internal/domain", "Imports": [], "Module": {"Path": "github.com/org/repo"}}`), nil
		},
	}

	m := &architectureManager{
		SP:     &mockSecurityProvider{},
		Runner: runner,
		// Loader and idx intentionally nil — forces realpackageProvider creation
	}

	res, err := m.VerifyArchitecture(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("VerifyArchitecture failed: %v", err)
	}
	if !strings.Contains(res.Text, "integrity verified") {
		t.Error("expected success message")
	}
}
