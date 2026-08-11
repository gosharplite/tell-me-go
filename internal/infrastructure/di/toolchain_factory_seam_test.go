// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	infra_tools "github.com/gosharplite/tell-me-go/internal/tools"
)

// TestBuildRegistry_PopulatesToolchainRunner is the per-destination wiring
// verification for the di composition root (issue #1325, ADR-060 seam
// capture): BuildRegistry must populate ToolchainRunner on the
// ToolRegistrationParams handed to RegisterAllTools.
func TestBuildRegistry_PopulatesToolchainRunner(t *testing.T) {
	sm := new(mockConfigurableSecurityManager)
	sm.RegisterPolicyToolsFunc = func(r tools.Registry, kv ports.KVStore) error { return nil }

	factory := newToolchainFactory(t.TempDir(), nil, sm, nil, nil, nil).(*defaultToolchainFactory)

	var captured infra_tools.ToolRegistrationParams
	factory.RegisterAllTools = func(params infra_tools.ToolRegistrationParams) error {
		captured = params
		return nil
	}
	factory.RegisterMetrics = func(r tools.Registry, sm security.Manager, logFile, traceFile, model, mode string, pricingOverrides map[string]pricing.ModelPricing) error {
		return nil
	}

	mockSP := &testfixtures.MockSessionProvider{
		GetSettingsFn: func() ports.KVStore { return &mockKVStore{} },
	}
	params := toolchainParams{
		Paths:           &persistence.Paths{},
		SessionProvider: mockSP,
	}

	if _, err := factory.BuildRegistry(params); err != nil {
		t.Fatalf("BuildRegistry failed: %v", err)
	}

	if captured.ToolchainRunner == nil {
		t.Fatal("ToolchainRunner was not populated on ToolRegistrationParams")
	}

	// Direct port call — never through the registry handler: check_vulnerabilities
	// falls through to a real govulncheck ./... via the non-injectable
	// devManager.executor (dev.go:327-331). CheckGovulncheck is lookPath-only;
	// the assertion is NO-PANIC — a NewGoRunner(nil-exec) construction typo
	// panics on the nil executor interface. Deterministic whether or not
	// govulncheck is installed (ADR-060 seam-capture rationale).
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("CheckGovulncheck panicked (runner constructed with nil executor?): %v", r)
			}
		}()
		_ = captured.ToolchainRunner.CheckGovulncheck(context.Background())
	}()
}
