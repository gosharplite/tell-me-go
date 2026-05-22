// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/session/sessiontest"
	"github.com/gosharplite/tell-me-go/internal/agent/session/ui"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/mock"
)

// Exported for external tests
type SessionManagerInternal = sessionManager
type SessionConfigInternal = sessionConfig
type SessionDependenciesInternal = sessiontest.Deps

func (o *sessionManager) ApplyConfiguration(ctx context.Context, chatAgent ports.Chatter, sCfg ports.SessionConfig, sd ports.SessionDependencies, capturer ports.Capturer) (*ui.Bridge, error) {
	return o.applyConfiguration(ctx, chatAgent, sCfg, sd, capturer)
}

func AsSessionManagerInternal(sm SessionManager) *sessionManager {
	return sm.(*sessionManager)
}

func SyncBridge(t *testing.T, b *ui.Bridge, m interface {
	On(methodName string, arguments ...interface{}) *mock.Call
}) {
	syncBridge(t, b, m)
}
