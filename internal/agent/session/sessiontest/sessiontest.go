// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package sessiontest provides backward-compatible aliases for session test
// helpers. These delegate to canonical implementations in agenttest.
//
// New code should import agenttest directly and use agenttest.StubSessionDependencies
// or agenttest.NewStubSessionDependencies.
package sessiontest

import (
	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
)

// Deps is an alias for agenttest.StubSessionDependencies.
//
// New code should use agenttest.StubSessionDependencies directly.
type Deps = agenttest.StubSessionDependencies

// New creates a new Deps with all required components.
//
// New code should use agenttest.NewStubSessionDependencies or construct
// &agenttest.StubSessionDependencies{...} directly.
var New = agenttest.NewStubSessionDependencies
