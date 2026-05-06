// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/require"
)

func TestTokenGatekeeper_ValidateHardLimits(t *testing.T) {
	tests := []struct {
		name      string
		maxTokens int
		tokens    int
		bus       events.EventBus
		logger    ports.Logger
		wantErr   error
		// extra runs additional assertions after the call
		extra func(t *testing.T, logger ports.Logger)
	}{
		{
			name:      "max_tokens_zero_skips_check",
			maxTokens: 0,
			tokens:    99999,
			wantErr:   nil,
		},
		{
			name:      "under_limit_passes",
			maxTokens: 10000,
			tokens:    8999,
			wantErr:   nil,
		},
		{
			name:      "over_limit_no_events_returns_error",
			maxTokens: 1000,
			tokens:    950,
			bus:       nil,
			wantErr:   llm.ErrContextLimitExceeded,
		},
		{
			name:      "over_limit_with_events_publishes_and_returns_error",
			maxTokens: 1000,
			tokens:    950,
			// Real event bus — validates the event-publishing path is entered.
			// We use sync dispatch so Publish completes inline.
			bus:     events.NewSimpleEventBus(context.Background(), events.WithAsync(false)),
			wantErr: llm.ErrContextLimitExceeded,
		},
		{
			name:      "over_limit_publish_failure_logs_error",
			maxTokens: 1000,
			tokens:    950,
			bus:       &agenttest.MockEventBusFail{PublishErr: errors.New("boom")},
			logger:    &agenttest.MockPortsLogger{},
			wantErr:   llm.ErrContextLimitExceeded,
			extra: func(t *testing.T, logger ports.Logger) {
				ml := logger.(*agenttest.MockPortsLogger)
				require.GreaterOrEqual(t, len(ml.Errors), 1, "expected at least one error logged")
				require.Contains(t, ml.Errors, "event_publish_failed")
			},
		},
		{
			name:      "over_limit_err_bus_not_initialized_swallowed",
			maxTokens: 1000,
			tokens:    950,
			bus:       &agenttest.MockEventBusFail{PublishErr: events.ErrBusNotInitialized},
			logger:    &agenttest.MockPortsLogger{},
			wantErr:   llm.ErrContextLimitExceeded,
			extra: func(t *testing.T, logger ports.Logger) {
				ml := logger.(*agenttest.MockPortsLogger)
				require.Empty(t, ml.Errors, "ErrBusNotInitialized should be swallowed, not logged")
			},
		},
		{
			name:      "small_context_10pct_cap",
			maxTokens: 1000,
			// 10% of 1000 = 100. SystemContextBuffer=1000, capped at 100. limit=900.
			// 901 > 900 => error.
			tokens:  901,
			wantErr: llm.ErrContextLimitExceeded,
		},
		{
			name:      "small_context_at_exact_10pct_boundary",
			maxTokens: 1000,
			// 10% of 1000 = 100. limit=900. 900 <= 900 => passes.
			tokens:  900,
			wantErr: nil,
		},
		{
			name:      "large_context_system_buffer_applies",
			maxTokens: 20000,
			// 10% of 20000 = 2000. SystemContextBuffer=1000 <= 2000, so reserved=1000.
			// limit=19000. 19001 > 19000 => error.
			tokens:  19001,
			wantErr: llm.ErrContextLimitExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tg := newTokenGatekeeper(
				nil,
				nil,
				withMaxTokens(tt.maxTokens),
				withEvents(tt.bus),
				withLogger(tt.logger),
			)

			ctx := context.Background()
			err := tg.validateHardLimits(ctx, nil, tt.tokens)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			if tt.extra != nil {
				tt.extra(t, tt.logger)
			}
		})
	}
}
