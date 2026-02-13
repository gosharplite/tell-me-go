// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
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

func (m *mockEstimator) EstimateTokens(contents []*llm.Content) int {
	return m.tokens
}

type mockHistoryManager struct {
	contents       []*llm.Content
	resolver       llm.AssetResolver
	setContentsErr error
}

func (m *mockHistoryManager) Load(ctx context.Context) error { return nil }
func (m *mockHistoryManager) Save(ctx context.Context) error { return nil }
func (m *mockHistoryManager) GetContents() []*llm.Content    { return m.contents }
func (m *mockHistoryManager) SetContents(ctx context.Context, contents []*llm.Content) error {
	if m.setContentsErr != nil {
		return m.setContentsErr
	}
	m.contents = contents
	return nil
}
func (m *mockHistoryManager) AddContent(ctx context.Context, content *llm.Content) error {
	m.contents = append(m.contents, content)
	return nil
}
func (m *mockHistoryManager) GetResolver() llm.AssetResolver { return m.resolver }
func (m *mockHistoryManager) SetPinned(ctx context.Context, turnIndex int, pinned bool) error {
	return nil
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
