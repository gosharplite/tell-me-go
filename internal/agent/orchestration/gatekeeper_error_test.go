package orchestration

import (
	"context"
	"errors"
	"flag"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type mockFailingSummarizer struct{}

func (m *mockFailingSummarizer) Summarize(ctx context.Context, history []*llm.Content, focus string) (string, *llm.Metrics, error) {
	return "", nil, errors.New("summarizer failed")
}

func (m *mockFailingSummarizer) SummarizeRange(ctx context.Context, turns int, focus string) (string, *llm.Metrics, error) {
	return "", nil, errors.New("summarizer failed")
}

func TestGatekeeper_ErrorHandling(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	req := &services.ContextRequest{
		History:  make([]*llm.Content, 20),
		Metadata: services.ContextMetadata{},
	}
	for i := 0; i < 20; i++ {
		role := "user"
		if i%2 != 0 {
			role = "model"
		}
		req.History[i] = &llm.Content{Role: role, Parts: []*llm.Part{{Text: "msg"}}}
	}

	gatekeeper := &tokenGatekeeper{
		MaxTokens:  100,
		Estimator:  &mockEstimator{tokens: 95},
		Summarizer: &mockFailingSummarizer{},
	}

	err := gatekeeper.Transform(ctx, req)
	if err == nil || err.Error() != "summarizer failed" {
		t.Errorf("Expected 'summarizer failed' error from handleSafetyPressure, got: %v", err)
	}

	req2 := &services.ContextRequest{
		History:  make([]*llm.Content, 20),
		Metadata: services.ContextMetadata{},
	}
	for i := 0; i < 20; i++ {
		role := "user"
		if i%2 != 0 {
			role = "model"
		}
		req2.History[i] = &llm.Content{Role: role, Parts: []*llm.Part{{Text: "msg"}}}
	}

	tc := &mockTokenCounter{tokens: 95}
	cs := NewContextStrategy(tc, nil)
	cs.setTieredThreshold(10)

	gatekeeper2 := &tokenGatekeeper{
		MaxTokens:  100,
		Estimator:  cs,
		Summarizer: &mockFailingSummarizer{},
	}

	err = gatekeeper2.Transform(ctx, req2)
	if err == nil || err.Error() != "summarizer failed" {
		t.Errorf("Expected 'summarizer failed' error from handleTieredThreshold, got: %v", err)
	}
}

func TestContextManager_FirstMessageRoleError(t *testing.T) {
	tc := &mockTokenCounter{tokens: 10}
	cs := NewContextStrategy(tc, nil)
	hm := &mockHistoryManager{}
	cm := NewContextManager(cs, hm, nil, nil)

	err := cm.AddContent(context.Background(), &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "first"}}})
	if err == nil || err.Error() != "first message must be 'user', got 'model'" {
		t.Errorf("Expected role error, got: %v", err)
	}
}

func TestContextTransformers_HistoryRepairerEmpty(t *testing.T) {
	hr := &historyRepairer{}
	req := &services.ContextRequest{History: nil}
	err := hr.Transform(context.Background(), req)
	if err != nil {
		t.Errorf("Expected nil error for empty history, got: %v", err)
	}
}

func TestInternalTools_Errors(t *testing.T) {
	tc := &mockTokenCounter{tokens: 10}
	cs := NewContextStrategy(tc, nil)
	hm := &mockHistoryManager{}
	cm := NewContextManager(cs, hm, nil, nil)

	it := NewInternalTools(cm)

	_, err := it.summarizeHistory(context.Background(), map[string]interface{}{"turns": "invalid"})
	if err == nil {
		t.Error("Expected error from unmarshal in summarizeHistory")
	}

	_, err = it.summarizeHistory(context.Background(), map[string]interface{}{"turns": float64(1)})
	if err == nil || err.Error() != "terminal error: summarizer not initialized" {
		t.Errorf("Expected summarizer error, got: %v", err)
	}

	_, err = it.ManageHistory(context.Background(), map[string]interface{}{"index": "invalid"})
	if err == nil {
		t.Error("Expected error from unmarshal in ManageHistory")
	}
}

type mockFailingChatter struct {
	err error
}

func (m *mockFailingChatter) Chat(ctx context.Context, session *services.Session, prompt string) error {
	return nil
}
func (m *mockFailingChatter) Shutdown(ctx context.Context) error   { return nil }
func (m *mockFailingChatter) Subscribe(handler func(events.Event)) {}
func (m *mockFailingChatter) SetLimits(ctx context.Context, maxToolTurns, contextWindow, maxHistoryTurns int) error {
	return m.err
}
func (m *mockFailingChatter) SetTieredThreshold(ctx context.Context, tieredThreshold int) error {
	return nil
}
func (m *mockFailingChatter) SetCostTracker(tracker domain_pricing.ICostTracker) {}
func (m *mockFailingChatter) GetName() string                                    { return "mock" }

type mockFailingCapturer struct{}

func (m *mockFailingCapturer) IsTTY(any) bool { return false }
func (m *mockFailingCapturer) CapturePrompt(context.Context, *flag.FlagSet, int, bool) (string, error) {
	return "", nil
}

type mockFailingUIRenderer struct{}

func (m *mockFailingUIRenderer) SetUseColor(bool)                {}
func (m *mockFailingUIRenderer) LogTurnStatus(events.TurnStatus) {}
func (m *mockFailingUIRenderer) StreamResponse(context.Context, bool, bool) (chan<- *llm.Content, func() *llm.Content) {
	return nil, nil
}
func (m *mockFailingUIRenderer) LogUsage(context.Context, *llm.Metrics, string, time.Time) {}
func (m *mockFailingUIRenderer) LogToolCall([]*llm.FunctionCall, int, int, bool)           {}
func (m *mockFailingUIRenderer) LogToolResult(string, tools.ToolResult, bool)              {}
func (m *mockFailingUIRenderer) LogSystemMessage(string, string)                           {}
func (m *mockFailingUIRenderer) LogAgentStatus(status string)                              {}

func TestOrchestrator_ConfigError(t *testing.T) {
	agentFactory := func(
		loader config.ConfigLoader,
		gw llm.LLMGateway,
		history services.HistoryManager,
		registry tools.IToolRegistry,
		sm security.ISecurityManager,
		disableStreaming bool,
		bus events.EventBus,
		provider, model, mode, logPath string,
		overrides map[string]domain_pricing.ModelPricing,
		tracker domain_pricing.ICostTracker,
	) services.Chatter {
		return &mockFailingChatter{err: errors.New("config failed")}
	}

	o := NewOrchestrator("", "", nil, nil, nil, nil, agentFactory, nil, &mockFailingUIRenderer{})

	cfg := &config.Config{
		SelectedProvider: "test",
	}
	sc := NewSessionConfig("", false, 0, false, "test prompt", cfg)

	ic := &mockFailingCapturer{}
	sd := NewSessionDependencies(&persistence.Paths{}, &mockHistoryManager{}, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, nil)

	err := o.Run(context.Background(), sc, sd, ic)
	if err == nil || err.Error() != "failed to apply configuration: config failed" {
		t.Errorf("Expected config failed error, got: %v", err)
	}
}
