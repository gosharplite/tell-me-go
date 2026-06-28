// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"errors"
	"testing"

	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSessionProvider is a minimal implementation of ports.SessionProvider
// for testing resolveActiveTools.
type stubSessionProvider struct {
	info ports.SessionInfo
}

func (s *stubSessionProvider) GetInfo() ports.SessionInfo            { return s.info }
func (s *stubSessionProvider) SetInfo(_ context.Context, info ports.SessionInfo) error {
	s.info = info
	return nil
}
func (s *stubSessionProvider) GetTasks() ports.TaskStore             { return nil }
func (s *stubSessionProvider) GetSettings() ports.KVStore            { return nil }
func (s *stubSessionProvider) GetHealthChecker() ports.HealthChecker { return nil }
func (s *stubSessionProvider) Close() error                          { return nil }

func TestResolveActiveTools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		turn     *Turn
		expected []string
	}{
		{
			name:     "nil Turn",
			turn:     nil,
			expected: nil,
		},
		{
			name:     "nil CtxManager",
			turn:     &Turn{},
			expected: nil,
		},
		{
			name: "CtxManager with nil SessionProvider",
			turn: &Turn{
				CtxManager: &sessctx.Manager{},
			},
			expected: nil,
		},
		{
			name: "SessionProvider with nil ActiveToolkits",
			turn: &Turn{
				CtxManager: &sessctx.Manager{
					SessionProvider: &stubSessionProvider{
						info: ports.SessionInfo{ActiveToolkits: nil},
					},
				},
			},
			expected: nil,
		},
		{
			name: "SessionProvider with empty toolkits",
			turn: &Turn{
				CtxManager: &sessctx.Manager{
					SessionProvider: &stubSessionProvider{
						info: ports.SessionInfo{ActiveToolkits: []string{}},
					},
				},
			},
			expected: []string{},
		},
		{
			name: "SessionProvider with populated toolkits",
			turn: &Turn{
				CtxManager: &sessctx.Manager{
					SessionProvider: &stubSessionProvider{
						info: ports.SessionInfo{ActiveToolkits: []string{"tk1", "tk2"}},
					},
				},
			},
			expected: []string{"tk1", "tk2"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveActiveTools(tt.turn)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestValidateResponse(t *testing.T) {
	t.Parallel()

	gatewayErr := errors.New("gateway timeout")
	rateLimitErr := errors.New("rate limited")

	tests := []struct {
		name        string
		content     *llm.Content
		err         error
		wantErr     bool
		errContains string
		// Optional: for the ErrLogic case, verify errors.Is
		wantErrLogic bool
		// Optional: for pass-through, verify exact input error
		wantInputErr error
	}{
		{
			name:         "nil content without error returns ErrLogic",
			content:      nil,
			err:          nil,
			wantErr:      true,
			errContains:  "api returned nil content",
			wantErrLogic: true,
		},
		{
			name:         "nil content with error passes error through",
			content:      nil,
			err:          gatewayErr,
			wantErr:      true,
			errContains:  "gateway timeout",
			wantInputErr: gatewayErr,
		},
		{
			name: "normal response with nil error",
			content: &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{Text: "hello"}},
			},
			err:     nil,
			wantErr: false,
		},
		{
			name: "content with gateway error passes error through",
			content: &llm.Content{
				Role: "model",
			},
			err:          rateLimitErr,
			wantErr:      true,
			errContains:  "rate limited",
			wantInputErr: rateLimitErr,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotErr := validateResponse(tt.content, tt.err)

			if !tt.wantErr {
				assert.NoError(t, gotErr)
				return
			}

			assert.Error(t, gotErr)
			assert.Contains(t, gotErr.Error(), tt.errContains)

			if tt.wantErrLogic {
				assert.ErrorIs(t, gotErr, ErrLogic)
			}

			if tt.wantInputErr != nil {
				assert.Equal(t, tt.wantInputErr, gotErr)
			}
		})
	}
}

func TestPublishInferenceStarted(t *testing.T) {
	t.Run("publishes event with correct model", func(t *testing.T) {
		bus := &eventstest.TestEventBus{}
		spyLogger := &testfixtures.SpyLogger{}
		turn := &Turn{
			Events: bus,
			Model:  "test-model",
			Logger: spyLogger,
		}
		ctx := context.Background()

		publishInferenceStarted(ctx, turn)

		recorded := bus.GetEvents()
		require.Len(t, recorded, 1)
		assert.Equal(t, "InferenceStartedEvent", recorded[0].Type())

		infEvent, ok := recorded[0].(events.InferenceStartedEvent)
		require.True(t, ok, "expected InferenceStartedEvent")
		assert.Equal(t, "test-model", infEvent.Model)

		assert.Empty(t, spyLogger.GetErrors())
	})

	t.Run("logs error when publish fails", func(t *testing.T) {
		bus := &eventstest.TestEventBus{}
		bus.SetPublishErr(errors.New("bus down"))
		spyLogger := &testfixtures.SpyLogger{}
		turn := &Turn{
			Events: bus,
			Model:  "test-model",
			Logger: spyLogger,
		}
		ctx := context.Background()

		// Function must not panic or return error — it logs instead.
		publishInferenceStarted(ctx, turn)

		assert.True(t, spyLogger.CalledWith("Error", "Failed to publish InferenceStartedEvent; UI may not show inference status"))
		assert.Empty(t, bus.GetEvents())
	})
}

func TestPublishResponseDetached(t *testing.T) {
	t.Run("publishes on cancelled context", func(t *testing.T) {
		bus := &eventstest.TestEventBus{}
		spyLogger := &testfixtures.SpyLogger{}
		turn := &Turn{
			Events: bus,
			Logger: spyLogger,
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // immediately cancel

		publishResponseDetached(ctx, turn, &llm.Content{
			Role:  "model",
			Parts: []*llm.Part{{Text: "ok"}},
		})

		recorded := bus.GetEvents()
		require.Len(t, recorded, 1)
		assert.Equal(t, "ResponseEvent", recorded[0].Type())

		respEvent, ok := recorded[0].(events.ResponseEvent)
		require.True(t, ok, "expected ResponseEvent")
		assert.Equal(t, "model", respEvent.Content.Role)
		require.Len(t, respEvent.Content.Parts, 1)
		assert.Equal(t, "ok", respEvent.Content.Parts[0].Text)

		assert.Empty(t, spyLogger.GetErrors())
	})

	t.Run("nil content replaced with model role sentinel", func(t *testing.T) {
		bus := &eventstest.TestEventBus{}
		spyLogger := &testfixtures.SpyLogger{}
		turn := &Turn{
			Events: bus,
			Logger: spyLogger,
		}

		publishResponseDetached(context.Background(), turn, nil)

		recorded := bus.GetEvents()
		require.Len(t, recorded, 1)
		assert.Equal(t, "ResponseEvent", recorded[0].Type())

		respEvent, ok := recorded[0].(events.ResponseEvent)
		require.True(t, ok, "expected ResponseEvent")
		assert.Equal(t, "model", respEvent.Content.Role)
	})

	t.Run("logs error when publish fails", func(t *testing.T) {
		bus := &eventstest.TestEventBus{}
		bus.SetPublishErr(errors.New("bus dead"))
		spyLogger := &testfixtures.SpyLogger{}
		turn := &Turn{
			Events: bus,
			Logger: spyLogger,
		}

		// Must not panic
		publishResponseDetached(context.Background(), turn, &llm.Content{Role: "model"})

		assert.True(t, spyLogger.CalledWith("Error", "Failed to publish ResponseEvent; UI spinner may hang"))
		assert.Empty(t, bus.GetEvents())
	})
}
