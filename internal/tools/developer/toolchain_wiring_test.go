// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package developer

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

// TestRegister_GetCoverage_ReachesFakeRunner proves devManager.runner is
// wired through the real handler dispatch (issue #1325, ADR-060): the fake
// ToolchainRunner's RunTestsWithCoverage must be reached via
// registry.Execute("get_coverage", ...). Zero subprocesses — the fake
// returns an empty CoverageSummary and the handler completes.
func TestRegister_GetCoverage_ReachesFakeRunner(t *testing.T) {
	fake := &toolstest.FakeToolchainRunner{}
	archVerify := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "ok"}, nil
	}
	reg := registry.New()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	if err := Register(reg, sm, fake, &toolstest.MockCommandValidator{}, persistence.NewMockFileSystem(), infra_persistence.NewWorkspacePolicy(), archVerify, &events.NoOpEventBus{}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	hb := make(chan struct{})
	if _, err := reg.Execute(context.Background(), "get_coverage", map[string]interface{}{"path": "./..."}, hb); err != nil {
		t.Fatalf("get_coverage failed: %v", err)
	}
	if !fake.Called("RunTestsWithCoverage") {
		t.Error("fake runner RunTestsWithCoverage was not reached through the get_coverage handler")
	}
}

// TestRegister_VerifyReleaseReadiness_ReachesFakeRunner proves
// releaseManager.runner is wired through the real handler dispatch: the fake
// ToolchainRunner's RunLinter/RunTests/BuildCode must be reached via
// registry.Execute("verify_release_readiness", ...). All six checks run
// concurrently; the remaining checks run against the mock FS and stub
// verifier — their PASS/FAIL is irrelevant, only the three runner
// assertions matter. Zero subprocesses.
func TestRegister_VerifyReleaseReadiness_ReachesFakeRunner(t *testing.T) {
	fake := &toolstest.FakeToolchainRunner{}
	archVerify := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "ok"}, nil
	}
	reg := registry.New()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	if err := Register(reg, sm, fake, &toolstest.MockCommandValidator{}, persistence.NewMockFileSystem(), infra_persistence.NewWorkspacePolicy(), archVerify, &events.NoOpEventBus{}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	hb := make(chan struct{})
	if _, err := reg.Execute(context.Background(), "verify_release_readiness", nil, hb); err != nil {
		t.Fatalf("verify_release_readiness failed: %v", err)
	}
	for _, method := range []string{"RunLinter", "RunTests", "BuildCode"} {
		if !fake.Called(method) {
			t.Errorf("fake runner %s was not reached through verify_release_readiness", method)
		}
	}
}
