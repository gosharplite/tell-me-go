// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/mock"
)

type mockToolRegistry struct {
	declarations []*tools.ToolDeclaration
}

func (m *mockToolRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return m.declarations
}

func (m *mockToolRegistry) Register(declaration *tools.ToolDeclaration, implementation tools.ToolFunc) {
	m.declarations = append(m.declarations, declaration)
}

func (m *mockToolRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) {
	m.Register(def, handler)
}

func (m *mockToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (m *mockToolRegistry) IsSerial(name string) bool {
	return false
}

func (m *mockToolRegistry) IsLongRunning(name string) bool {
	return false
}

type mockSummarizer struct {
	summarizeFn func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error)
}

func (m *mockSummarizer) Summarize(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
	if m.summarizeFn != nil {
		return m.summarizeFn(ctx, subset, focus)
	}
	return "summary", &llm.Metrics{}, nil
}

type mockTokenCounter struct {
	tokens int
}

func (m *mockTokenCounter) Count(contents []*llm.Content) int {
	return m.tokens
}

type mockEstimator struct {
	tokens int
}

func (m *mockEstimator) estimateTokens(contents []*llm.Content) int {
	return m.tokens
}

type mockHistoryManager struct {
	contents       []*llm.Content
	resolver       llm.AssetResolver
	setContentsErr error
	getWindowErr   error
	rollbackErr    error
}

func (m *mockHistoryManager) GetTotalEntries() int { return len(m.contents) }
func (m *mockHistoryManager) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	if m.getWindowErr != nil {
		return nil, m.getWindowErr
	}
	total := len(m.contents)
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx > total {
		startIdx = total
	}
	if endIdx == -1 || endIdx > total {
		endIdx = total
	}
	if endIdx < startIdx {
		return []*llm.Content{}, nil
	}

	window := m.contents[startIdx:endIdx]
	cloned := make([]*llm.Content, len(window))
	for i, c := range window {
		cloned[i] = llm.CloneContent(c)
	}
	return cloned, nil
}
func (m *mockHistoryManager) SetContents(ctx context.Context, contents []*llm.Content) error {
	if m.setContentsErr != nil {
		return m.setContentsErr
	}
	m.contents = contents
	return nil
}
func (m *mockHistoryManager) Archive(ctx context.Context, contents []*llm.Content) error {
	return nil
}
func (m *mockHistoryManager) AddContent(ctx context.Context, content *llm.Content) error {
	m.contents = append(m.contents, content)
	return nil
}
func (m *mockHistoryManager) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	if index >= 0 && index < len(m.contents) {
		m.contents[index].Parts = append(m.contents[index].Parts, parts...)
	}
	return nil
}
func (m *mockHistoryManager) GetResolver() llm.AssetResolver { return m.resolver }
func (m *mockHistoryManager) SetPinned(ctx context.Context, turnIndex int, pinned bool) error {
	return nil
}
func (m *mockHistoryManager) Save(ctx context.Context) error { return nil }
func (m *mockHistoryManager) RollbackTurns(ctx context.Context, turns int) (int, int, int, error) {
	if m.rollbackErr != nil {
		return 0, 0, 0, m.rollbackErr
	}
	originalLen := len(m.contents)
	if originalLen == 0 || turns <= 0 {
		return 0, originalLen / 2, originalLen, nil
	}

	removeMsgs := turns * 2
	var actualRemoved int
	if removeMsgs >= originalLen {
		actualRemoved = originalLen / 2
		m.contents = nil
	} else {
		actualRemoved = turns
		m.contents = m.contents[:originalLen-removeMsgs]
	}

	remainingMsgs := len(m.contents)
	remainingTurns := remainingMsgs / 2

	return actualRemoved, remainingTurns, remainingMsgs, nil
}

type mockGateway struct {
	generateFn func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error))
	sendChatFn func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
}

func (m *mockGateway) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
	if m.generateFn != nil {
		return m.generateFn(ctx, input, tools, resolver)
	}
	ch := make(chan *llm.Content)
	close(ch)
	return ch, func() (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
	}
}

func (m *mockGateway) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.sendChatFn != nil {
		return m.sendChatFn(ctx, history, tools, resolver)
	}
	return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
}

func (m *mockGateway) StreamChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	callback(&llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}})
	return &llm.Metrics{}, nil
}

func (m *mockGateway) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return [][]byte{}, nil
}

func (m *mockGateway) RefreshAuth() error { return nil }

type mockConfigLoader struct {
	mock.Mock
}

func (m *mockConfigLoader) Load(path string) (*config.Config, error) {
	args := m.Called(path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*config.Config), args.Error(1)
}

type mockEventBus struct {
	events []events.Event
}

func (m *mockEventBus) Publish(ctx context.Context, e events.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.events = append(m.events, e)
	return nil
}

func (m *mockEventBus) Subscribe(f func(events.Event))     {}
func (m *mockEventBus) Shutdown(ctx context.Context) error { return nil }
func (m *mockEventBus) Flush(ctx context.Context) error    { return nil }

type mockTransformer struct {
	priority    int
	transformFn func(ctx context.Context, req *ports.ContextRequest) error
}

func (m *mockTransformer) Transform(ctx context.Context, req *ports.ContextRequest) error {
	if m.transformFn != nil {
		return m.transformFn(ctx, req)
	}
	return nil
}

func (m *mockTransformer) Priority() int {
	return m.priority
}

type mockPruningPolicy struct {
	markTurnsFn func(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error)
	nameFn      func() string
}

func (m *mockPruningPolicy) MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error) {
	if m.markTurnsFn != nil {
		return m.markTurnsFn(ctx, turns, keep)
	}
	return 0, nil
}

func (m *mockPruningPolicy) Name() string {
	if m.nameFn != nil {
		return m.nameFn()
	}
	return "MockPolicy"
}
