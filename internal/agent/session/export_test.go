// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session/ui"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// Exported for external tests
type SessionManagerInternal = sessionManager
type SessionConfigInternal = sessionConfig
type SessionDependenciesInternal = agenttest.StubChatterComposer

func (o *sessionManager) ApplyConfiguration(ctx context.Context, chatAgent ports.Chatter, sCfg ports.SessionConfig, sd ports.ChatterComposer, capturer ports.Capturer) (*ui.Bridge, error) {
	return o.applyConfiguration(ctx, chatAgent, sCfg, sd, capturer)
}

func AsSessionManagerInternal(sm SessionManager) *sessionManager {
	return sm.(*sessionManager)
}

func SyncBridge(t *testing.T, b *ui.Bridge, m *agenttest.MockUIRenderer) {
	syncBridge(t, b, m)
}
