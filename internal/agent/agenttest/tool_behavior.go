// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ToolBehavior describes the scripted behaviour of a tool used in
// executor table tests: result, error, latency, panic value, and
// scheduling flags. Observe is a callback fired when the tool runs,
// useful for asserting execution order.
type ToolBehavior struct {
	Result  tools.ToolResult
	Err     error
	Delay   time.Duration
	Panic   interface{}
	Serial  bool
	Long    bool
	Observe func() // Callback to signal execution
}
