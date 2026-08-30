// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"context"
	"io"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
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

	factory := newToolchainFactory(t.TempDir(), nil, sm, nil, nil, nil, nil).(*defaultToolchainFactory)

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

// TestBuildRegistry_PopulatesProcessRunner is the wiring verification for
// the di composition root's process-runner field (issue #1460, ADR-074 seam
// capture, mirroring the ToolchainRunner probe): BuildRegistry must populate
// ProcessRunner on the ToolRegistrationParams handed to RegisterAllTools.
func TestBuildRegistry_PopulatesProcessRunner(t *testing.T) {
	sm := new(mockConfigurableSecurityManager)
	sm.RegisterPolicyToolsFunc = func(r tools.Registry, kv ports.KVStore) error { return nil }

	factory := newToolchainFactory(t.TempDir(), nil, sm, nil, nil, nil, nil).(*defaultToolchainFactory)

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

	if captured.ProcessRunner == nil {
		t.Fatal("ProcessRunner was not populated on ToolRegistrationParams")
	}

	// Direct port call — never through the registry handler: a dropped
	// `regParams.ProcessRunner = newProcessRunner()` compiles (nil satisfies
	// the interface), so the behavioral call is the enforcement. Starts the
	// real `true` process — safe, deterministic, environment-independent —
	// and drains Stdout to EOF before Wait (the runner's handle contract).
	// The assertion is NO-PANIC on Start plus a clean Wait.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Start panicked (ProcessRunner not populated with a functioning adapter?): %v", r)
			}
		}()
		h, err := captured.ProcessRunner.Start(context.Background(), tools.ProcessSpec{Name: "true", Args: nil})
		if err != nil {
			t.Fatalf("Start(%q) returned error: %v", "true", err)
		}
		if h == nil {
			t.Fatal("Start returned nil handle with nil error")
		}
		_, _ = io.Copy(io.Discard, h.Stdout())
		_, _ = io.Copy(io.Discard, h.Stderr())
		if err := h.Wait(); err != nil {
			t.Fatalf("Wait() returned error for the `true` process: %v", err)
		}
	}()
}

// TestBuildRegistry_PopulatesMCPClients verifies the toolchain-factory hop:
// toolchainParams.MCPServers is resolved by the MCP factory and attached to
// ToolRegistrationParams.MCPClients (issue #1373).
func TestBuildRegistry_PopulatesMCPClients(t *testing.T) {
	sm := new(mockConfigurableSecurityManager)
	sm.RegisterPolicyToolsFunc = func(r tools.Registry, kv ports.KVStore) error { return nil }

	factory := newToolchainFactory(t.TempDir(), nil, sm, nil, nil, nil, nil).(*defaultToolchainFactory)

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
		MCPServers: map[string]config.MCPServerConfig{
			"readonly": {URL: "https://example.com/mcp/readonly"},
		},
	}

	if _, err := factory.BuildRegistry(params); err != nil {
		t.Fatalf("BuildRegistry failed: %v", err)
	}

	dep, ok := captured.MCPClients["readonly"]
	if !ok {
		t.Fatal("MCPClients did not contain 'readonly'")
	}
	if dep.Client == nil {
		t.Error("MCP client was not constructed")
	}
	if dep.RequiresConsent {
		t.Error("readonly endpoint should not require consent")
	}
	if dep.Serial {
		t.Error("readonly endpoint should not be serial")
	}
}
