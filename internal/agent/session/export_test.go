// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/stretchr/testify/mock"
)

// Export for testing
type (
	TokenGatekeeper           = tokenGatekeeper
	Request                   = request
	FileConfigWatcher         = fileConfigWatcher
	NoOpConfigWatcherInternal = noOpConfigWatcher
	HistoryPruner             = historyPruner
	SlidingWindowPolicy       = slidingWindowPolicy
	WarningInjector           = warningInjector
	TransientMerger           = transientMerger
	HistoryRepairer           = historyRepairer
	SessionConfigInternal     = sessionConfig
	SessionDependenciesInternal = sessionDependencies
	SessionManagerInternal     = sessionManager
	UIBridge                  = uiBridge
)

func (tg *tokenGatekeeper) AutoSummarize(ctx context.Context, req *ports.ContextRequest) (bool, error) {
	n, err := tg.autoSummarize(ctx, req)
	return n > 0, err
}

func (tg *tokenGatekeeper) FindSummarizableRange(ctx context.Context, history []*llm.Content) (int, int, int, error) {
	return tg.findSummarizableRange(ctx, history)
}

func (cm *ContextManager) GetVersion() int {
	return cm.version
}

func (cm *ContextManager) SetLogger(l ports.Logger) {
	cm.logger = l
}

// Mocks
type (
	MockTokenCounter    = mockTokenCounter
	MockSummarizer      = mockSummarizer
	MockUIRenderer      = mockUIRenderer
	MockEstimator       = mockEstimator
	MockConfigLoader    = mockConfigLoader
	MockHistoryManager  = mockHistoryManager
	MockGateway         = mockGateway
	MockToolRegistry    = mockToolRegistry
	MockTransformer     = mockTransformer
	MockChatter         = mockChatter
	MockCapturer        = mockCapturer
	MockHistoryRenderer = mockHistoryRenderer
	MockEntropySource   = mockEntropySource
	MockClock           = mockClock
	MockSessionProvider  = mockSessionProvider
	MockEventBus        = mockEventBus
	MockTurnsLogger     = mockTurnsLogger
)

func (m *mockTokenCounter) SetTokens(n int) {
	m.tokens = n
}

func (m *mockEstimator) SetTokens(n int) {
	m.tokens = n
}

func (m *mockSummarizer) SetSummarizeFn(fn func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error)) {
	m.summarizeFn = fn
}

func (m *mockHistoryManager) SetInternalContents(c []*llm.Content) {
	m.contents = c
}

func (m *mockHistoryManager) GetContents() []*llm.Content {
	return m.contents
}

func (m *mockHistoryManager) SetGetWindowErr(err error) {
	m.getWindowErr = err
}

func (m *mockHistoryManager) SetSetContentsErr(err error) {
	m.setContentsErr = err
}

func (m *mockHistoryManager) SetRollbackErr(err error) {
	m.rollbackErr = err
}

func (m *mockGateway) SetSendChatFn(fn func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)) {
	m.sendChatFn = fn
}

func (m *mockGateway) SetGenerateFn(fn func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)) {
	m.generateFn = fn
}

func (m *mockTransformer) SetTransformFn(fn func(ctx context.Context, req *ports.ContextRequest) error) {
	m.transformFn = fn
}

var ErrInvalidPayload = errInvalidPayload

type SyncWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	Writer  io.Writer
	OnWrite chan struct{}
}

func (w *SyncWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Writer != nil {
		n, err = w.Writer.Write(p)
	} else {
		n, err = w.buf.Write(p)
	}
	if w.OnWrite != nil {
		select {
		case w.OnWrite <- struct{}{}:
		default:
		}
	}
	return n, err
}

func (b *uiBridge) HandleEvent(ctx context.Context, e events.Event) error {
	return b.handleEvent(ctx, e)
}

func SyncBridge(t *testing.T, b *uiBridge, m *mockUIRenderer) {
	t.Helper()
	// Use a sentinel event that is handled by the bridge and calls a mock method.
	// LogSystemMessage is ideal as it's safe to call when no spinner is active.
	done := make(chan struct{})
	m.On("LogSystemMessage", mock.Anything, "SYNC_SENTINEL", "info").Run(func(_ mock.Arguments) {
		close(done)
	}).Return().Once()

	// Use a non-polling send with timeout to guarantee delivery without flakiness.
	select {
	case b.eventCh <- events.SystemMessageEvent{Message: "SYNC_SENTINEL", Level: "info"}:
	case <-time.After(2 * time.Second):
		t.Fatal("Failed to queue sync sentinel (queue full?)")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for sync sentinel processing")
	}
}

func (w *SyncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func (cw *fileConfigWatcher) GetContextWindow() int {
	return cw.contextWindow
}

func (cw *fileConfigWatcher) GetDefaultWindow() int {
	return cw.defaultWindow
}

func (cw *fileConfigWatcher) SetTieredThresholdInternal(t int) {
	cw.tieredThreshold = t
}

func (cs *ContextStrategy) SetTieredThreshold(t int) {
	cs.setTieredThreshold(t)
}

func (cs *ContextStrategy) SetContextWindow(w int) {
	cs.setContextWindow(w)
}

func (cs *ContextStrategy) GetContextWindow() int {
	return cs.contextWindow
}

func (cs *ContextStrategy) GetMaxHistoryTokens() int {
	return cs.maxHistoryTokens
}

func (cs *ContextStrategy) GetMaxToolTurns() int {
	return cs.maxToolTurns
}

func (t *InternalTools) SummarizeHistory(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return t.summarizeHistory(ctx, args, hb)
}

const PriorityTransientThreshold = priorityTransientThreshold

func NewSessionManager(homeDir, version string, loader config.ConfigLoader, sm domain_security.Manager, stdout, stderr io.Writer, factory ports.ChatterFactory, historyRenderer ports.HistoryRenderer, uiRenderer ports.UIRenderer, clk clock.Clock, entropy io.Reader) SessionManager {
	return newSessionManager(homeDir, version, loader, sm, stdout, stderr, factory, historyRenderer, uiRenderer, clk, entropy)
}

func NewSessionConfig(configPath string, newSession bool, lastN, backN int, rawOutput bool, prompt string, cfg *config.Config) ports.SessionConfig {
	return newSessionConfig(configPath, newSession, lastN, backN, rawOutput, prompt, cfg)
}

func NewSessionDependencies(paths *persistence.Paths, hManager ports.HistoryManager, client llm.LLMClient, gw llm.LLMGateway, reg tools.Registry, sm domain_security.Manager, tracker domain_pricing.CostTracker, pData domain_pricing.PricingData, overrides map[string]domain_pricing.ModelPricing, bus events.EventBus, logger ports.Logger, turnsLogger ports.TurnsLogger, sessionProvider ports.SessionProvider) ports.SessionDependencies {
	return newSessionDependencies(paths, hManager, client, gw, reg, sm, tracker, pData, overrides, bus, logger, turnsLogger, sessionProvider)
}

func (m *mockToolRegistry) SetRegisterErr(err error) {
	m.registerErr = err
}

func (m *mockToolRegistry) SetFailAfter(n int) {
	m.failAfter = n
}

func (o *sessionManager) ApplyConfiguration(ctx context.Context, chatAgent ports.Chatter, sCfg ports.SessionConfig, sd ports.SessionDependencies, capturer ports.Capturer) (*UIBridge, error) {
	return o.applyConfiguration(ctx, chatAgent, sCfg, sd, capturer)
}

func (b *uiBridge) Wg() *sync.WaitGroup {
	return &b.wg
}

func NewUIBridge(renderer ports.UIRenderer, opts ...bridgeOption) *uiBridge {
	return newUIBridge(renderer, opts...)
}

func WithBridgeThoughts(show bool) bridgeOption {
	return withBridgeThoughts(show)
}

func WithBridgeTools(show bool) bridgeOption {
	return withBridgeTools(show)
}

func WithBridgeRawOutput(raw bool) bridgeOption {
	return withBridgeRawOutput(raw)
}

func WithBridgeColor(color bool) bridgeOption {
	return withBridgeColor(color)
}

func WithBridgeLogFile(path string) bridgeOption {
	return withBridgeLogFile(path)
}

func WithBridgeLogger(l ports.Logger) bridgeOption {
	return withBridgeLogger(l)
}

func WithBridgeClock(c clock.Clock) bridgeOption {
	return withBridgeClock(c)
}
