// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package sessiontest provides backward-compatible aliases for session test
// helpers. These delegate to canonical implementations in agenttest.
//
// New code should import agenttest directly and use
// agenttest.NewStubSessionDependencies or construct
// &agenttest.StubSessionDependencies{...} directly.
package sessiontest

import (
	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
)

// New creates a new StubSessionDependencies with all required components.
//
// New code should use agenttest.NewStubSessionDependencies directly.
var New = agenttest.NewStubSessionDependencies
