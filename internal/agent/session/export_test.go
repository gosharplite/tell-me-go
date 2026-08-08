// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session/ui"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// Exported for external tests
type SessionManagerInternal = sessionManager
type SessionConfigInternal = sessionConfig

// SessionDependenciesInternal is a legacy test alias kept for ADR-056:
// renaming churns ~15 test sites for zero production value.
type SessionDependenciesInternal = agenttest.StubChatterComposer

func (o *sessionManager) ApplyConfiguration(ctx context.Context, chatAgent ports.Chatter, sCfg SessionConfig, sd ports.ChatterComposer, capturer ports.Capturer, tuiOutput bool) (*ui.Bridge, error) {
	return o.applyConfiguration(ctx, chatAgent, sCfg, sd, capturer, tuiOutput)
}

func (o *sessionManager) RenderPostTUISummary(ts events.TurnStatus, sd ports.ChatterComposer, sc SessionConfig, ic ports.Capturer) {
	o.renderPostTUISummary(ts, sd, sc, ic)
}

func AsSessionManagerInternal(sm SessionManager) *sessionManager {
	return sm.(*sessionManager)
}

func SyncBridge(t *testing.T, b *ui.Bridge, m *agenttest.MockUIRenderer) {
	syncBridge(t, b, m)
}
