// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Re-export or use the domain executor
type CommandExecutor = tools.CommandExecutor
type RealExecutor = executor.RealExecutor
