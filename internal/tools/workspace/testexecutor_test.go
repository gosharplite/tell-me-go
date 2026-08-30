// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	infra_process "github.com/gosharplite/tell-me-go/internal/infrastructure/process"
)

// newTestProcessExecutor is the singular in-package test constructor for the
// processExecutor (ADR-074 Decision 6 — the workspace analog of ADR-060 §9's
// surviving real-adapter test sites): it defaults to the REAL filesystem
// adapter and the REAL process adapter, replacing the deleted zero-arg
// newprocessExecutor(). Tests that exercise runner behavior construct
// newprocessExecutorWithFS(fs, fakeRunner) explicitly — choosing the fake is
// the assertion. Test files may import infrastructure (the
// verify-tools-adapter-import and verify-tools-process-import gates are
// production-scoped; ADR-074 anti-extension ruling).
func newTestProcessExecutor() *processExecutor {
	return newprocessExecutorWithFS(infra_persistence.NewOSFileSystem(), infra_process.NewRunner())
}
