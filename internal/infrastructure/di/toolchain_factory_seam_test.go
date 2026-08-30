// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"context"
	"io"
	"strings"
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

// TestBuildRegistry_InstallSkillExecutesRealRunnerClosure drives the real
// execRunner closure (toolchain_factory.go:159-161, issue #1462 B3): the
// composition root hands osexec.CommandContext(...).CombinedOutput() to
// NewSkillManager, and the registered install_skill tool is the only path
// that invokes it. A pre-cancelled context makes the closure fail
// deterministically without meaningful network activity — the assertion
// holds whether or not git is installed (missing git fails at LookPath
// inside CombinedOutput). Only the InstallSkill wrap ("cloning repository")
// is asserted, never the underlying git/LookPath error text.
func TestBuildRegistry_InstallSkillExecutesRealRunnerClosure(t *testing.T) {
	sm := new(mockConfigurableSecurityManager)
	sm.RegisterPolicyToolsFunc = func(r tools.Registry, kv ports.KVStore) error { return nil }

	factory := newToolchainFactory(t.TempDir(), nil, sm, nil, nil, nil, nil).(*defaultToolchainFactory)
	factory.RegisterAllTools = func(params infra_tools.ToolRegistrationParams) error { return nil }
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

	reg, err := factory.BuildRegistry(params)
	if err != nil {
		t.Fatalf("BuildRegistry failed: %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	defer cancel()

	res, err := reg.Execute(canceled, "install_skill", map[string]interface{}{
		"repo_url": "https://github.com/tmgo-test/tmgo-no-such-repo-b3",
	}, nil)
	if err != nil {
		t.Fatalf("Execute(install_skill) returned a Go error; want the failure surfaced via ToolResult.Error: %v", err)
	}
	if res.Error == nil {
		t.Fatal("ToolResult.Error = nil; want the InstallSkill failure surfaced via ToolResult.Error")
	}
	if !strings.Contains(res.Error.Error(), "cloning repository") {
		t.Errorf("ToolResult.Error = %v; want the 'cloning repository' wrap from InstallSkill", res.Error)
	}
}
