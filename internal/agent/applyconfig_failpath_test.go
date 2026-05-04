// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// stubConfigWatcher implements session.ConfigWatcher with canned return
// values for GetLimits(). Used by TestApplyConfig_FailFastChain to inject
// valid or invalid limits into the delegate chain without modifying
// production ConfigWatcher implementations.
// ---------------------------------------------------------------------------

type stubConfigWatcher struct {
	tokens       int
	toolTurns    int
	historyTurns int
}

func (s *stubConfigWatcher) SetPaths(_, _ string)               {}
func (s *stubConfigWatcher) Refresh(_ string)                   {}
func (s *stubConfigWatcher) SetLimits(_, _, _ int)              {}
func (s *stubConfigWatcher) GetLimits() (int, int, int)         { return s.tokens, s.toolTurns, s.historyTurns }
func (s *stubConfigWatcher) ApplyLimits(_ events.Limits)        {}
func (s *stubConfigWatcher) SyncToStrategy(_ *sessctx.Strategy) {}

// ---------------------------------------------------------------------------
// TestApplyConfig_FailFastChain
// ---------------------------------------------------------------------------

// TestApplyConfig_FailFastChain verifies the ADR-029 fail-fast delegate
// chain contract: applyConfig invokes SafePublish → Engine.Reconfigure →
// Manager.Reconfigure in fixed order, and any delegate's error short-
// circuits the chain. This test is the structural proof of the contract
// documented in (*agent).applyConfig and ADR-029 §3.
//
// Approach: Option A — real delegate failure via input validation
// (T2's Validate() rules). For Engine failure we SetRuntimeConfigForInternalUse
// with an empty ProviderName, which RuntimeConfig.Validate() rejects.
// For Manager failure we replace the configWatcher with a stub returning
// negative MaxToolTurns, which Limits.Validate() rejects.
//
// The "manager called / not called" assertion combines two signals:
//  1. Did GetLimits() change? A delta proves Manager.Reconfigure
//     executed successfully (Strategy.SetLimits mutated state).
//  2. Does the error message implicate the context manager? If so,
//     Manager was reached and returned an error (called but failed).
//
// Manager.Reconfigure is considered "called" when either signal fires.
// This avoids false negatives when validation failure leaves Manager
// state identical to pre-call state.
func TestApplyConfig_FailFastChain(t *testing.T) {
	tests := []struct {
		name              string
		setup             func(t *testing.T) (ports.Chatter, InternalAccessor)
		wantErr           bool
		wantErrSubstr     string
		wantManagerCalled bool
	}{
		{
			name: "valid config: all delegates succeed",
			setup: func(t *testing.T) (ports.Chatter, InternalAccessor) {
				chatter, accessor := newTestAgent(t)

				// Replace configWatcher with a stub returning limits that
				// differ from the defaults (120000/200/0). The delta in
				// GetLimits() across the ApplyConfig call proves that
				// Manager.Reconfigure executed.
				accessor.SetConfigWatcherForInternalUse(&stubConfigWatcher{
					tokens:       240000,
					toolTurns:    400,
					historyTurns: 100,
				})
				return chatter, accessor
			},
			wantErr:           false,
			wantManagerCalled: true,
		},
		{
			name: "engine validation failure: manager not called",
			setup: func(t *testing.T) (ports.Chatter, InternalAccessor) {
				chatter, accessor := newTestAgent(t)

				// Empty ProviderName → RuntimeConfig.Validate() rejects →
				// Engine.Reconfigure returns error → Manager.Reconfigure
				// structurally skipped (the fail-fast contract).
				accessor.SetRuntimeConfigForInternalUse(
					"", // empty ProviderName
					"test-model",
					"test-mode",
					nil, // pricing overrides
					events.Limits{
						MaxHistoryTokens: 120000,
						MaxToolTurns:     200,
						MaxHistoryTurns:  0,
					},
				)
				return chatter, accessor
			},
			wantErr:           true,
			wantErrSubstr:     "engine reconfigure",
			wantManagerCalled: false,
		},
		{
			name: "context manager validation failure",
			setup: func(t *testing.T) (ports.Chatter, InternalAccessor) {
				chatter, accessor := newTestAgent(t)

				// Negative MaxToolTurns from configWatcher → Limits.Validate()
				// rejects → Manager.Reconfigure returns error.
				// Engine.Reconfigure succeeds because ProviderName/Model/Mode
				// remain valid.
				accessor.SetConfigWatcherForInternalUse(&stubConfigWatcher{
					tokens:       120000,
					toolTurns:    -1, // negative → validation failure
					historyTurns: 0,
				})
				return chatter, accessor
			},
			wantErr:           true,
			wantErrSubstr:     "context manager reconfigure",
			wantManagerCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatter, accessor := tt.setup(t)

			// Snapshot Manager state BEFORE applyConfig.
			preLimits := accessor.GetCtxManagerForInternalUse().GetLimits()

			err := accessor.ApplyConfig(context.Background())

			// Snapshot Manager state AFTER applyConfig.
			postLimits := accessor.GetCtxManagerForInternalUse().GetLimits()

			// --- Error assertions ---
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrSubstr)
				}
				if !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Errorf("error = %q; want substring %q", err.Error(), tt.wantErrSubstr)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}

			// --- Structural contract: was Manager.Reconfigure invoked? ---
			//
			// We combine two signals:
			//   a) Did GetLimits() change?  A delta proves Manager.Reconfigure
			//      executed and mutated internal state.
			//   b) Does the error implicate the context manager?  If so,
			//      Manager was reached and returned an error (called but
			//      failed — limits unchanged).
			limitsChanged := !reflect.DeepEqual(preLimits, postLimits)
			managerCalled := limitsChanged ||
				(err != nil && strings.Contains(err.Error(), "context manager reconfigure"))

			if managerCalled != tt.wantManagerCalled {
				t.Errorf("Manager.Reconfigure called = %v; want %v "+
					"(pre-limits=%+v, post-limits=%+v, limits-changed=%v, err=%v)",
					managerCalled, tt.wantManagerCalled,
					preLimits, postLimits, limitsChanged, err)
			}

			_ = chatter // keep alive
		})
	}
}

// newTestAgent constructs a valid, fully-initialized agent and returns
// both the Chatter interface (to keep the agent alive) and the
// InternalAccessor (for state inspection and mutation).
func newTestAgent(t *testing.T) (ports.Chatter, InternalAccessor) {
	t.Helper()

	ctx := context.Background()
	gw := &agenttest.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := agenttest.NewMockToolRegistry()
	sm := &mockSecurityManager{AllowAll: true}

	chatter, err := NewAgent(gw, bus, reg,
		WithSecurityManager(sm),
		WithProviderName("test-provider"),
		WithPricing("test-model", "test-mode", nil),
	)
	require.NoError(t, err)

	accessor := AsInternal(chatter)
	require.NotNil(t, accessor, "AsInternal must return a non-nil accessor for the production *agent type")

	// Sanity: the agent was fully initialized and has a context manager.
	require.NotNil(t, accessor.GetCtxManagerForInternalUse(),
		"context manager must be non-nil after NewAgent")

	return chatter, accessor
}

// Ensure stubConfigWatcher satisfies the interface at compile time.
var _ session.ConfigWatcher = (*stubConfigWatcher)(nil)
