// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type mockToolRegistry struct {
	declarations []*tools.ToolDeclaration
}

func (m *mockToolRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return m.declarations
}

func (m *mockToolRegistry) Register(declaration *tools.ToolDeclaration, implementation tools.ToolFunc) {
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

type mockTokenCounter struct {
	tokens int
}

func (m *mockTokenCounter) Count(contents []*llm.Content) int {
	return m.tokens
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

func (m *mockGateway) SetSystemInstructions(instr string) {}

type MockSecurityManager struct {
	AllowAll bool
}

func (m *MockSecurityManager) ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error) {
	return true, nil
}
func (m *MockSecurityManager) IsPathSafe(path string) (string, error)     { return path, nil }
func (m *MockSecurityManager) IsPathWritable(path string) (string, error) { return path, nil }
func (m *MockSecurityManager) TerminalLock()                              {}
func (m *MockSecurityManager) TerminalUnlock()                            {}
func (m *MockSecurityManager) IsBypassActive() bool                       { return true }
func (m *MockSecurityManager) IsCommandAllowed(command string) bool {
	return m.AllowAll
}
