// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatService_Initialization_Failures(t *testing.T) {
	errBuild := errors.New("build error")

	tests := []struct {
		name    string
		setup   func(sf *agenttest.MockSessionLifecycleManager)
		wantErr string
	}{
		{
			name: "Build Session Dependencies Failure",
			setup: func(sf *agenttest.MockSessionLifecycleManager) {
				sf.On("BuildSessionDependencies", context.Background(), &config.Config{}, "config.yaml", false, nil).Return(nil, nil, func(context.Context) error { return nil }, errBuild)
			},
			wantErr: "build error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := &agenttest.MockServiceSecurityManager{}
			mockSF := &agenttest.MockSessionLifecycleManager{}
			if tt.setup != nil {
				tt.setup(mockSF)
			}

			service := agent.NewChatService(
				"home", "v1", io.Discard, io.Discard, mockSM,
				mockSF, nil, &agenttest.StubUIRenderer{}, &agenttest.StubHistoryRenderer{}, &agenttest.StubHistoryBrowser{}, nil,
			)

			// 3. Attempt ProcessMessage
			cfg := &config.Config{}
			cmd := agent.ChatCommand{ConfigPath: "config.yaml"}
			err := service.ProcessMessage(context.Background(), cfg, cmd, nil)

			// 4. Assert exact failure
			require.Error(t, err, "expected initialization to fail")
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
