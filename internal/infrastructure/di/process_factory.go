// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_process "github.com/gosharplite/tell-me-go/internal/infrastructure/process"
)

// newProcessRunner is the single production construction of the process
// runner (issue #1460, ADR-074): the raw-exec lifecycle class in tools is
// eliminated; only the di composition root constructs. Mirrors the
// ToolchainRunner construction in toolchain_factory.go; the runner is
// threaded into the tools layer via ToolRegistrationParams (T4).
func newProcessRunner() tools.ProcessRunner {
	return infra_process.NewRunner()
}

// Compile-time guard: newProcessRunner satisfies tools.ProcessRunner. Keeps
// the unused linter green until the runner is wired into BuildRegistry; left
// in place afterwards as self-documenting.
var _ tools.ProcessRunner = newProcessRunner()
