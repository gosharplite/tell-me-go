// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"os"
	"path/filepath"
	"testing"

	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

// TestNewAnalysisManager_WiresRunnerIntoAllConsumers is a white-box
// construction assertion: the runner injected into newAnalysisManager must
// reach every consumer field (info, arch, health, dependency). No tool
// execution, no subprocesses.
func TestNewAnalysisManager_WiresRunnerIntoAllConsumers(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\ngo 1.25"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte("package test\nfunc F(){}\nvar _ = F"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, _ := newIndexer(tmpDir)
	idx.knownModulePath = "test"
	cache := newASTCache(".")
	sp := &mockSecurityProvider{}
	mockExec := &mockHealthExecutor{}
	fs := persistencetest.NewPlainOSFileSystem()
	wp := infra_persistence.NewWorkspacePolicy()
	dc := newDeadCodeAnalyzer(sp, idx)

	fake := &toolstest.FakeToolchainRunner{}
	m := newAnalysisManager(idx, cache, sp, nil, mockExec, fake, fs, wp, dc)

	if m.info.Runner == nil {
		t.Error("infoManager.Runner is nil; runner not wired")
	}
	if m.arch.Runner == nil {
		t.Error("architectureManager.Runner is nil; runner not wired")
	}
	if m.health.Runner == nil {
		t.Error("healthManager.Runner is nil; runner not wired")
	}
	dep, ok := m.dependency.(*defaultDependencyAnalyzer)
	if !ok {
		t.Fatalf("dependency field is %T, want *defaultDependencyAnalyzer", m.dependency)
	}
	if dep.Runner == nil {
		t.Error("dependencyAnalyzer.Runner is nil; runner not wired")
	}
}
