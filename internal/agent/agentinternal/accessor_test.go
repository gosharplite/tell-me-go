// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agentinternal

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mockInternalAccessor is a mock of agent.InternalAccessor used by
// white-box tests of the agentinternal delegation chain. It mirrors
// the testify/mock pattern established by mockSessionLifecycleManager.
type mockInternalAccessor struct {
	mock.Mock
}

func (m *mockInternalAccessor) ApplyConfig(ctx context.Context) error {
	return m.Called(ctx).Error(0)
}

func (m *mockInternalAccessor) DiffConfig() *agent.DriftReport {
	return m.Called().Get(0).(*agent.DriftReport)
}

func (m *mockInternalAccessor) AsChatter() ports.Chatter {
	return m.Called().Get(0).(ports.Chatter)
}

func (m *mockInternalAccessor) GetTrackerForInternalUse() domain_pricing.CostTracker {
	return m.Called().Get(0).(domain_pricing.CostTracker)
}

func (m *mockInternalAccessor) GetCtxManagerForInternalUse() *sessctx.Manager {
	return m.Called().Get(0).(*sessctx.Manager)
}

func (m *mockInternalAccessor) GetEventsForInternalUse() events.EventBus {
	return m.Called().Get(0).(events.EventBus)
}

func (m *mockInternalAccessor) GetConfigWatcherForInternalUse() domain_config.ConfigWatcher {
	return m.Called().Get(0).(domain_config.ConfigWatcher)
}

func (m *mockInternalAccessor) GetRuntimeSnapshotForInternalUse() struct {
	ProviderName     string
	Model            string
	Mode             string
	PricingOverrides map[string]domain_pricing.ModelPricing
	Limits           events.Limits
} {
	return m.Called().Get(0).(struct {
		ProviderName     string
		Model            string
		Mode             string
		PricingOverrides map[string]domain_pricing.ModelPricing
		Limits           events.Limits
	})
}

func (m *mockInternalAccessor) SetEventsForInternalUse(bus events.EventBus) {
	m.Called(bus)
}

func (m *mockInternalAccessor) SetConfigWatcherForInternalUse(cw domain_config.ConfigWatcher) {
	m.Called(cw)
}

func (m *mockInternalAccessor) SetCtxManagerForInternalUse(cm *sessctx.Manager) {
	m.Called(cm)
}

func (m *mockInternalAccessor) SetLoggerForInternalUse(l ports.Logger) {
	m.Called(l)
}

func (m *mockInternalAccessor) SetTrackerForInternalUse(t domain_pricing.CostTracker) {
	m.Called(t)
}

func (m *mockInternalAccessor) SetRuntimeConfigForInternalUse(
	providerName, model, mode string,
	pricingOverrides map[string]domain_pricing.ModelPricing,
	limits events.Limits,
) {
	m.Called(providerName, model, mode, pricingOverrides, limits)
}

var _ agent.InternalAccessor = (*mockInternalAccessor)(nil)

// TestAsAgentInternal verifies the constructor that wraps a
// ports.Chatter into an *AgentInternal. It must return nil for a
// nil input and a valid, non-nil *AgentInternal for a bare agent.
func TestAsAgentInternal(t *testing.T) {
	t.Run("nil chatter", func(t *testing.T) {
		result := AsAgentInternal(nil)
		if result != nil {
			t.Errorf("AsAgentInternal(nil) = %v; want nil", result)
		}
	})

	t.Run("bare agent", func(t *testing.T) {
		bare := agent.NewBareForInternalUse()
		chatter := bare.AsChatter()
		result := AsAgentInternal(chatter)
		if result == nil {
			t.Fatal("AsAgentInternal(bare agent) returned nil")
			return
		}
		if result.raw == nil {
			t.Fatal("AsAgentInternal(bare agent).raw is nil")
		}
	})
}

// TestNewBareAgent verifies that NewBareAgent returns a non-nil
// *AgentInternal with a non-nil underlying raw accessor.
func TestNewBareAgent(t *testing.T) {
	a := NewBareAgent()
	if a == nil {
		t.Fatal("NewBareAgent() returned nil")
		return
	}
	if a.raw == nil {
		t.Fatal("NewBareAgent().raw is nil")
	}
}

// mockCostTracker is a mock of domain_pricing.CostTracker for use in
// getter delegation tests. It follows the testify/mock pattern used
// by mockInternalAccessor and mockSessionLifecycleManager.
type mockCostTracker struct {
	mock.Mock
}

func (m *mockCostTracker) GetTotalCost(ctx context.Context) float64 {
	return m.Called(ctx).Get(0).(float64)
}

func (m *mockCostTracker) GetDailyCost(ctx context.Context) float64 {
	return m.Called(ctx).Get(0).(float64)
}

func (m *mockCostTracker) GetStats(ctx context.Context) (domain_pricing.UsageStats, float64) {
	args := m.Called(ctx)
	return args.Get(0).(domain_pricing.UsageStats), args.Get(1).(float64)
}

func (m *mockCostTracker) Accumulate(mt llm.Metrics) {
	m.Called(mt)
}

func (m *mockCostTracker) AccumulateAndReturn(mt llm.Metrics) float64 {
	return m.Called(mt).Get(0).(float64)
}

func (m *mockCostTracker) Warmup() {
	m.Called()
}

var _ domain_pricing.CostTracker = (*mockCostTracker)(nil)

// shutdowner is a minimal interface for types that support graceful
// shutdown. It is used to clean up event buses created during tests.
type shutdowner interface {
	Shutdown(ctx context.Context) error
}

// TestAgentInternal_Getters verifies that each read-only accessor on
// *AgentInternal correctly delegates to the corresponding
// *ForInternalUse bridge method on the underlying InternalAccessor.
// Each sub-test injects a concrete value into a mock, calls the
// accessor, and asserts the returned value is identical.
func TestAgentInternal_Getters(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(mock *mockInternalAccessor) any
		call   func(ai *AgentInternal) any
		assert func(t *testing.T, got, want any)
	}{
		{
			name: "GetCtxManager",
			setup: func(mock *mockInternalAccessor) any {
				mgr := &sessctx.Manager{}
				mock.On("GetCtxManagerForInternalUse").Return(mgr)
				return mgr
			},
			call: func(ai *AgentInternal) any {
				return ai.GetCtxManager()
			},
			assert: assertPointerEqual,
		},
		{
			name: "GetEvents",
			setup: func(mock *mockInternalAccessor) any {
				bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
				mock.On("GetEventsForInternalUse").Return(bus)
				return bus
			},
			call: func(ai *AgentInternal) any {
				return ai.GetEvents()
			},
			assert: assertPointerEqual,
		},
		{
			name: "GetConfigWatcher",
			setup: func(mock *mockInternalAccessor) any {
				cw := domain_config.NewNoOpConfigWatcher(1000, 10, 5)
				mock.On("GetConfigWatcherForInternalUse").Return(cw)
				return cw
			},
			call: func(ai *AgentInternal) any {
				return ai.GetConfigWatcher()
			},
			assert: assertPointerEqual,
		},
		{
			name: "GetTracker",
			setup: func(mock *mockInternalAccessor) any {
				tr := &mockCostTracker{}
				mock.On("GetTrackerForInternalUse").Return(tr)
				return tr
			},
			call: func(ai *AgentInternal) any {
				return ai.GetTracker()
			},
			assert: assertPointerEqual,
		},
		{
			name: "GetRuntimeConfig",
			setup: func(mock *mockInternalAccessor) any {
				snap := struct {
					ProviderName     string
					Model            string
					Mode             string
					PricingOverrides map[string]domain_pricing.ModelPricing
					Limits           events.Limits
				}{
					ProviderName: "test-provider",
					Model:        "test-model",
					Mode:         "test-mode",
					PricingOverrides: map[string]domain_pricing.ModelPricing{
						"test-model": {Hit: 0.01},
					},
					Limits: events.Limits{
						MaxHistoryTokens: 2000,
						MaxToolTurns:     20,
						MaxHistoryTurns:  10,
					},
				}
				mock.On("GetRuntimeSnapshotForInternalUse").Return(snap)
				return RuntimeSnapshot{
					ProviderName: "test-provider",
					Model:        "test-model",
					Mode:         "test-mode",
					PricingOverrides: map[string]domain_pricing.ModelPricing{
						"test-model": {Hit: 0.01},
					},
					Limits: events.Limits{
						MaxHistoryTokens: 2000,
						MaxToolTurns:     20,
						MaxHistoryTurns:  10,
					},
				}
			},
			call: func(ai *AgentInternal) any {
				return ai.GetRuntimeConfig()
			},
			assert: func(t *testing.T, got, want any) {
				t.Helper()
				if !reflect.DeepEqual(got, want) {
					t.Errorf("GetRuntimeConfig() = %+v; want %+v", got, want)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := new(mockInternalAccessor)
			want := tt.setup(mock)
			ai := &AgentInternal{raw: mock}

			// Shut down any event bus created during setup.
			if s, ok := want.(shutdowner); ok {
				t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
			}

			got := tt.call(ai)
			tt.assert(t, got, want)
			mock.AssertExpectations(t)
		})
	}
}

// assertPointerEqual is a helper that uses pointer equality (==) to
// verify that a getter returned the exact same value injected into
// the mock. It is appropriate for pointer and interface types where
// identity matters.
func assertPointerEqual(t *testing.T, got, want any) {
	t.Helper()
	if got != want {
		t.Errorf("got %v; want %v", got, want)
	}
}

// TestAgentInternal_Setters verifies that each *ForTest mutator on
// *AgentInternal correctly delegates to the corresponding
// Set*ForInternalUse bridge method on the underlying InternalAccessor.
// Each sub-test sets up a mock expectation with the exact argument,
// invokes the mutator, and asserts the expectation was satisfied.
func TestAgentInternal_Setters(t *testing.T) {
	tests := []struct {
		name  string
		setup func(mock *mockInternalAccessor) any
		call  func(ai *AgentInternal, val any)
	}{
		{
			name: "SetEventsForTest",
			setup: func(mock *mockInternalAccessor) any {
				bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
				mock.On("SetEventsForInternalUse", bus).Return()
				return bus
			},
			call: func(ai *AgentInternal, val any) {
				ai.SetEventsForTest(val.(events.EventBus))
			},
		},
		{
			name: "SetConfigWatcherForTest",
			setup: func(mock *mockInternalAccessor) any {
				cw := domain_config.NewNoOpConfigWatcher(1000, 10, 5)
				mock.On("SetConfigWatcherForInternalUse", cw).Return()
				return cw
			},
			call: func(ai *AgentInternal, val any) {
				ai.SetConfigWatcherForTest(val.(domain_config.ConfigWatcher))
			},
		},
		{
			name: "SetCtxManagerForTest",
			setup: func(mock *mockInternalAccessor) any {
				mgr := &sessctx.Manager{}
				mock.On("SetCtxManagerForInternalUse", mgr).Return()
				return mgr
			},
			call: func(ai *AgentInternal, val any) {
				ai.SetCtxManagerForTest(val.(*sessctx.Manager))
			},
		},
		{
			name: "SetLoggerForTest",
			setup: func(mock *mockInternalAccessor) any {
				l := slog.Default()
				mock.On("SetLoggerForInternalUse", l).Return()
				return l
			},
			call: func(ai *AgentInternal, val any) {
				ai.SetLoggerForTest(val.(ports.Logger))
			},
		},
		{
			name: "SetTrackerForTest",
			setup: func(mock *mockInternalAccessor) any {
				tr := &mockCostTracker{}
				mock.On("SetTrackerForInternalUse", tr).Return()
				return tr
			},
			call: func(ai *AgentInternal, val any) {
				ai.SetTrackerForTest(val.(domain_pricing.CostTracker))
			},
		},
		{
			name: "SetRuntimeConfigForTest",
			setup: func(mock *mockInternalAccessor) any {
				snap := RuntimeSnapshot{
					ProviderName: "p",
					Model:        "m",
					Mode:         "md",
					PricingOverrides: map[string]domain_pricing.ModelPricing{
						"x": {},
					},
					Limits: events.Limits{MaxHistoryTokens: 100},
				}
				mock.On("SetRuntimeConfigForInternalUse",
					snap.ProviderName, snap.Model, snap.Mode,
					snap.PricingOverrides, snap.Limits,
				).Return()
				return snap
			},
			call: func(ai *AgentInternal, val any) {
				ai.SetRuntimeConfigForTest(val.(RuntimeSnapshot))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := new(mockInternalAccessor)
			val := tt.setup(mock)
			ai := &AgentInternal{raw: mock}

			// Shut down any event bus created during setup.
			if s, ok := val.(shutdowner); ok {
				t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
			}

			tt.call(ai, val)
			mock.AssertExpectations(t)
		})
	}
}

// mockChatter is a mock of ports.Chatter used by action delegation
// tests that need to verify Chat and Shutdown pass through the
// AsChatter bridge correctly.
type mockChatter struct {
	mock.Mock
}

func (m *mockChatter) Chat(ctx context.Context, sess *ports.Session, prompt string) error {
	return m.Called(ctx, sess, prompt).Error(0)
}

func (m *mockChatter) Shutdown(ctx context.Context) error {
	return m.Called(ctx).Error(0)
}

func (m *mockChatter) SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error {
	return m.Called(ctx, toolTurns, historyTokens, historyTurns).Error(0)
}

func (m *mockChatter) Subscribe(sub func(context.Context, events.Event)) {
	m.Called(sub)
}

var _ ports.Chatter = (*mockChatter)(nil)

// TestAgentInternal_Actions verifies that the three action methods —
// ApplyConfig, Chat, and Shutdown — correctly delegate to the
// underlying InternalAccessor bridge and propagate errors.
func TestAgentInternal_Actions(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, mockRaw *mockInternalAccessor, mockCh *mockChatter)
		call    func(ai *AgentInternal) error
		wantErr bool
	}{
		{
			name: "ApplyConfig/success",
			setup: func(t *testing.T, mockRaw *mockInternalAccessor, mockCh *mockChatter) {
				mockRaw.On("ApplyConfig", mock.Anything).Return(nil)
			},
			call: func(ai *AgentInternal) error {
				return ai.ApplyConfig(context.Background())
			},
			wantErr: false,
		},
		{
			name: "ApplyConfig/error",
			setup: func(t *testing.T, mockRaw *mockInternalAccessor, mockCh *mockChatter) {
				mockRaw.On("ApplyConfig", mock.Anything).Return(assert.AnError)
			},
			call: func(ai *AgentInternal) error {
				return ai.ApplyConfig(context.Background())
			},
			wantErr: true,
		},
		{
			name: "Chat/success",
			setup: func(t *testing.T, mockRaw *mockInternalAccessor, mockCh *mockChatter) {
				mockRaw.On("AsChatter").Return(mockCh)
				mockCh.On("Chat", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			call: func(ai *AgentInternal) error {
				return ai.Chat(context.Background(), &ports.Session{}, "test prompt")
			},
			wantErr: false,
		},
		{
			name: "Chat/error",
			setup: func(t *testing.T, mockRaw *mockInternalAccessor, mockCh *mockChatter) {
				mockRaw.On("AsChatter").Return(mockCh)
				mockCh.On("Chat", mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)
			},
			call: func(ai *AgentInternal) error {
				return ai.Chat(context.Background(), &ports.Session{}, "test prompt")
			},
			wantErr: true,
		},
		{
			name: "Shutdown/success",
			setup: func(t *testing.T, mockRaw *mockInternalAccessor, mockCh *mockChatter) {
				mockRaw.On("AsChatter").Return(mockCh)
				mockCh.On("Shutdown", mock.Anything).Return(nil)
			},
			call: func(ai *AgentInternal) error {
				return ai.Shutdown(context.Background())
			},
			wantErr: false,
		},
		{
			name: "Shutdown/error",
			setup: func(t *testing.T, mockRaw *mockInternalAccessor, mockCh *mockChatter) {
				mockRaw.On("AsChatter").Return(mockCh)
				mockCh.On("Shutdown", mock.Anything).Return(assert.AnError)
			},
			call: func(ai *AgentInternal) error {
				return ai.Shutdown(context.Background())
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaw := new(mockInternalAccessor)
			mockCh := new(mockChatter)

			tt.setup(t, mockRaw, mockCh)

			ai := &AgentInternal{raw: mockRaw}
			err := tt.call(ai)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			mockRaw.AssertExpectations(t)
			mockCh.AssertExpectations(t)
		})
	}
}

func TestMockSessionLifecycleManager_BuildSessionDeps(t *testing.T) {
	m := new(MockSessionLifecycleManager)
	m.BuildSessionDepsFunc = func(ctx context.Context, cfg *domain_config.Config, configPath string, newSession bool, capturer agent.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
		return nil, nil, nil, nil
	}
	_, _, _, err := m.BuildSessionDependencies(context.Background(), nil, "", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	snap := m.Snapshot()
	if snap["BuildSessionDependencies"] != 1 {
		t.Errorf("BuildSessionDependencies calls: got %d, want 1", snap["BuildSessionDependencies"])
	}
	if snap["FinalizeSession"] != 0 {
		t.Errorf("FinalizeSession calls: got %d, want 0", snap["FinalizeSession"])
	}
}

func TestMockSessionLifecycleManager_BuildSessionDeps_ReturnsValues(t *testing.T) {
	m := new(MockSessionLifecycleManager)
	wantCleanup := func(context.Context) error { return nil }
	m.BuildSessionDepsFunc = func(ctx context.Context, cfg *domain_config.Config, configPath string, newSession bool, capturer agent.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
		return nil, nil, wantCleanup, nil
	}
	_, _, cleanup, err := m.BuildSessionDependencies(context.Background(), nil, "", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cleanup == nil {
		t.Fatal("cleanup should not be nil")
	}
}

func TestMockSessionLifecycleManager_BuildSessionDeps_ReturnsError(t *testing.T) {
	m := new(MockSessionLifecycleManager)
	wantErr := errors.New("build failed")
	m.BuildSessionDepsFunc = func(ctx context.Context, cfg *domain_config.Config, configPath string, newSession bool, capturer agent.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
		return nil, nil, nil, wantErr
	}
	_, _, _, err := m.BuildSessionDependencies(context.Background(), nil, "", false, nil)
	if err != wantErr {
		t.Errorf("got %v; want %v", err, wantErr)
	}
}

func TestMockSessionLifecycleManager_FinalizeSession(t *testing.T) {
	m := new(MockSessionLifecycleManager)
	m.FinalizeSessionFunc = func(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionFinalizer, cfg *domain_config.Config) error {
		return nil
	}
	err := m.FinalizeSession(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	snap := m.Snapshot()
	if snap["FinalizeSession"] != 1 {
		t.Errorf("FinalizeSession calls: got %d, want 1", snap["FinalizeSession"])
	}
}

func TestMockSessionLifecycleManager_FinalizeSession_Error(t *testing.T) {
	m := new(MockSessionLifecycleManager)
	wantErr := errors.New("finalize failed")
	m.FinalizeSessionFunc = func(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionFinalizer, cfg *domain_config.Config) error {
		return wantErr
	}
	err := m.FinalizeSession(context.Background(), nil, nil, nil)
	if err != wantErr {
		t.Errorf("got %v; want %v", err, wantErr)
	}
}

func TestMockSessionLifecycleManager_DefaultBehavior(t *testing.T) {
	m := new(MockSessionLifecycleManager)
	deps, hm, cleanup, err := m.BuildSessionDependencies(context.Background(), nil, "", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deps != nil || hm != nil || cleanup != nil {
		t.Error("expected nil returns when Fn is nil")
	}
	err = m.FinalizeSession(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
