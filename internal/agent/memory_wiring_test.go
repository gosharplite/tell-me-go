// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
)

// newMemoryTestAgent constructs a fully-initialized agent with a SpyLogger
// plus the given options, returning the Chatter interface, the white-box
// *agent, and the logger for assertion.
func newMemoryTestAgent(t *testing.T, opts ...AgentOption) (ports.Chatter, *agent, *testfixtures.SpyLogger) {
	t.Helper()

	ctx := context.Background()
	gw := &agenttest.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := agenttest.NewMockToolRegistry()
	sm := &mockSecurityManager{AllowAll: true}
	spy := &testfixtures.SpyLogger{}

	all := append([]AgentOption{
		WithSecurityManager(sm),
		WithProviderName("test-provider"),
		WithPricing("test-model", "test-mode", nil),
		WithLogger(spy),
	}, opts...)

	chatter, err := NewAgent(gw, bus, reg, all...)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}
	return chatter, chatter.(*agent), spy
}

// TestNewAgent_MemoryEnabledAbsentServer_WarnsAndDisables verifies the
// two-stage fallback (ADR-068 §5): an enabled-but-absent server (nil MCP
// client with a non-empty seed SERVER) yields a successful NewAgent, a
// memory_server_unavailable warn, an effectively-disabled shared config
// snapshot, and a constructed (inert, nil-client) hook. Structural wiring
// proof (injector in the pipeline, hook firing) is deferred to the T7 E2E;
// this test proves construction + fail-open posture.
func TestNewAgent_MemoryEnabledAbsentServer_WarnsAndDisables(t *testing.T) {
	seed := domain_config.MemoryConfig{
		Enabled:             true,
		Server:              "plur",
		InjectBudget:        2000,
		LearnTier:           domain_config.MemoryLearnBatch,
		MaxLearnsPerSession: 3,
	}

	_, a, spy := newMemoryTestAgent(t, WithMemoryClient(nil, seed))

	if !spy.CalledWith("Warn", "memory_server_unavailable") {
		t.Error("expected memory_server_unavailable warn for enabled-but-absent server")
	}
	// Effective disable: the shared snapshot must be non-nil and disabled.
	if cfg := a.memoryCfg.Load(); cfg == nil || cfg.Enabled {
		t.Errorf("memoryCfg snapshot = %+v; want non-nil with Enabled=false", cfg)
	}
	// The hook was constructed (once, at initComponents) with the nil client.
	if a.memoryHook == nil {
		t.Error("expected memoryHook to be constructed for enabled-but-absent server")
	}
}

// TestNewAgent_NoMemory_NoWarnNoComponents verifies the default posture:
// without WithMemoryClient the agent constructs NO memory components, emits
// no memory_server_unavailable warn, and leaves memoryHook nil — zero
// behavior change for existing users (ADR-068 §5).
func TestNewAgent_NoMemory_NoWarnNoComponents(t *testing.T) {
	_, a, spy := newMemoryTestAgent(t) // default: no memory options

	if spy.CalledWith("Warn", "memory_server_unavailable") {
		t.Error("expected NO memory_server_unavailable warn when memory is not configured")
	}
	if a.memoryHook != nil {
		t.Error("expected memoryHook nil when memory is not configured")
	}
}

// ---------------------------------------------------------------------------
// T4: Chat session-end memory write-failure surfacing (issue #1410 §4)
// ---------------------------------------------------------------------------

// rejectingMCPClient is a hand-rolled tools.MCPClient stub that rejects every
// CallTool with a ToolResult.Error and a nil Go error — the real isError
// convention from the MCP adapter (issue #1410).
type rejectingMCPClient struct{}

func (rejectingMCPClient) ListTools(ctx context.Context) ([]tools.MCPToolDefinition, error) {
	return nil, nil
}
func (rejectingMCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	return tools.ToolResult{Text: "rejected", Error: errors.New("rejected")}, nil
}
func (rejectingMCPClient) Close() error { return nil }

// recordingTurnsLogger is a hand-rolled ports.TurnsLogger that captures
// SystemMessageEvent messages (the turns.log surface) so tests can assert the
// report content surfaced by Chat's defer. Listen returns nil on ctx cancel —
// mirroring the production asyncTurnsLogger — so Chat returns nil after a
// clean run (telemetryErr joins nil).
type recordingTurnsLogger struct {
	mu       sync.Mutex
	messages []string
}

func (l *recordingTurnsLogger) HandleEvent(ctx context.Context, e events.Event) {
	if sme, ok := e.(events.SystemMessageEvent); ok {
		l.mu.Lock()
		l.messages = append(l.messages, sme.Message)
		l.mu.Unlock()
	}
}

func (l *recordingTurnsLogger) Listen(ctx context.Context) error { <-ctx.Done(); return nil }
func (l *recordingTurnsLogger) Close() error                     { return nil }

// messagesSnapshot returns a copy of the captured SystemMessageEvent messages.
func (l *recordingTurnsLogger) messagesSnapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.messages...)
}

// memoryStubConfigWatcher is a hand-rolled domain_config.ConfigWatcher whose
// GetMemoryConfig returns the seeded MEMORY config. The agent's
// prepareRuntimeConfig reads the hot-reload MEMORY snapshot from the watcher
// every turn (the seed in WithMemoryClient only pre-populates the atomic
// before the first applyConfig), so this stub is required to keep the runtime
// tier: the default NoOp watcher returns DefaultMemoryConfig (disabled, batch),
// which would suppress the injector and flatten both tiers to batch.
type memoryStubConfigWatcher struct {
	memory domain_config.MemoryConfig
}

func (w *memoryStubConfigWatcher) SetPaths(_, _ string)        {}
func (w *memoryStubConfigWatcher) Refresh(_ string)            {}
func (w *memoryStubConfigWatcher) SetLimits(_, _, _ int)       {}
func (w *memoryStubConfigWatcher) GetLimits() (int, int, int)  { return 120000, 200, 0 }
func (w *memoryStubConfigWatcher) GetContextWindow() int       { return 1000000 }
func (w *memoryStubConfigWatcher) ApplyLimits(_ events.Limits) {}
func (w *memoryStubConfigWatcher) GetMemoryConfig() domain_config.MemoryConfig {
	return w.memory
}

var _ domain_config.ConfigWatcher = (*memoryStubConfigWatcher)(nil)

// TestChat_MemoryWriteFailuresSurfacedAtSessionEnd drives Chat at agent level
// with a rejecting MCP client and proves the whole write-failure chain
// (ADR-068 §2.3, issue #1410 §4): rejected write → counted in writeStats →
// surfaced by Chat's top-of-Chat flush-then-read defer via BOTH the turns.log
// SystemMessageEvent (best-effort primary) and the synchronous stderr Warn
// "memory_write_failures" (asserted surface). Table over two tiers:
//
//   - capture: the per-turn plur_capture write is rejected and counted; the
//     defer's FlushSession is a no-op (no ring buffer) and the report names
//     plur_capture.
//   - batch: the per-turn episode is buffered, and the defer's FlushSession
//     flushes it via plur_learn_batch, which is rejected and counted — the
//     report naming plur_learn_batch proves flush-then-read through the real
//     defer (the report includes the flush's own attempt).
//
// The injector (Seam A) also fires per turn and is rejected
// (memory_injection_failed Warn) but carries no write counters — extra Warns
// are expected and harmless; assertions target memory_write_failures only.
// Exactly one prompt/turn keeps the failure count exactly 1.
func TestChat_MemoryWriteFailuresSurfacedAtSessionEnd(t *testing.T) {
	tests := []struct {
		name     string
		tier     domain_config.MemoryLearnTier
		wantTool string
	}{
		{
			name:     "capture tier — per-turn plur_capture rejected",
			tier:     domain_config.MemoryLearnCapture,
			wantTool: "plur_capture",
		},
		{
			name:     "batch tier — plur_learn_batch rejected at flush through the real defer",
			tier:     domain_config.MemoryLearnBatch,
			wantTool: "plur_learn_batch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seed := domain_config.MemoryConfig{
				Enabled:             true,
				Server:              "plur",
				InjectBudget:        2000,
				LearnTier:           tt.tier,
				MaxLearnsPerSession: 3,
			}

			hManager := &agenttest.MockHistoryManager{}
			turns := &recordingTurnsLogger{}
			watcher := &memoryStubConfigWatcher{memory: seed}

			_, a, spy := newMemoryTestAgent(t,
				WithHistoryManager(hManager),
				WithConfigWatcher(watcher),
				WithTurnsLogger(turns),
				WithMemoryClient(rejectingMCPClient{}, seed),
			)

			ctx := context.Background()
			session := ports.NewSession("s1", hManager)

			if err := a.Chat(ctx, session, "hello"); err != nil {
				t.Fatalf("Chat returned error: %v", err)
			}

			// Asserted surface: the synchronous stderr Warn fires at session
			// end. testfixtures.SpyLogger records only the message (never the
			// key-value args), so the report string is asserted via the turns
			// logger below.
			if !spy.CalledWith("Warn", "memory_write_failures") {
				t.Error("expected memory_write_failures Warn at session end")
			}

			// Best-effort primary surface: the same report rides the turns
			// logger as a SystemMessageEvent. Assert the failure count is
			// exact and the dead tool is named.
			var report string
			for _, msg := range turns.messagesSnapshot() {
				if strings.Contains(msg, "memory write failures: 1") {
					report = msg
					break
				}
			}
			if report == "" {
				t.Fatalf("expected turns.log SystemMessageEvent with 'memory write failures: 1', got %q",
					turns.messagesSnapshot())
			}
			if !strings.Contains(report, tt.wantTool+" failing — learning is disabled") {
				t.Errorf("report %q: want dead tool %q named", report, tt.wantTool)
			}
		})
	}
}
