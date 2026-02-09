// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/exec"
)

// Re-export or use the domain executor
type CommandExecutor = tools.CommandExecutor
type RealExecutor = exec.RealExecutor
