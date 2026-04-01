// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/skills"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type mockToolRegistry struct {
	Declarations []*tools.ToolDeclaration
}

func (m *mockToolRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return m.Declarations
}

func (m *mockToolRegistry) Register(declaration *tools.ToolDeclaration, implementation tools.ToolFunc) error {
	return m.RegisterToToolkit("core", declaration, implementation)
}

func (m *mockToolRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.RegisterToToolkitWithOptions("core", def, handler, opts)
}

func (m *mockToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (m *mockToolRegistry) IsSerial(name string) bool {
	return false
}

func (m *mockToolRegistry) IsLongRunning(name string) bool {
	return false
}

func (m *mockToolRegistry) GetOptions(name string) tools.ToolOptions {
	return tools.ToolOptions{Serial: m.IsSerial(name), LongRunning: m.IsLongRunning(name)}
}

func (m *mockToolRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return m.RegisterToToolkitWithOptions(toolkit, def, handler, tools.ToolOptions{})
}

func (m *mockToolRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	m.Declarations = append(m.Declarations, def)
	return nil
}

func (m *mockToolRegistry) GetCoreDeclarations() []*tools.ToolDeclaration {
	return m.GetDeclarations()
}

func (m *mockToolRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	return m.GetDeclarations()
}

func (m *mockToolRegistry) ListAvailableToolkits() []string {
	return []string{"core"}
}

type mockTokenCounter struct {
	tokens int
}

func (m *mockTokenCounter) Count(contents []*llm.Content) int {
	return m.tokens
}

func (m *mockTokenCounter) CountTokens(text string) int {
	return m.tokens
}

type mockGateway struct {
	GenerateFunc func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	sendChatFn   func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
}

func (m *mockGateway) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, input, tools, resolver)
	}
	return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
}

func (m *mockGateway) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.sendChatFn != nil {
		return m.sendChatFn(ctx, history, tools, resolver)
	}
	return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
}

func (m *mockGateway) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return [][]byte{}, nil
}

func (m *mockGateway) RefreshAuth() error { return nil }

type mockSecurityManager struct {
	domain_security.Manager
	AllowAll bool
}

func (m *mockSecurityManager) IsPathSafe(path string) (string, error) { return path, nil }
func (m *mockSecurityManager) TerminalLock()                          {}
func (m *mockSecurityManager) TerminalUnlock()                        {}
func (m *mockSecurityManager) IsCommandAllowed(command string) bool {
	return m.AllowAll
}

func (m *mockSecurityManager) Close() error { return nil }

type mockSummarizer struct {
	ports.Summarizer
}

type mockLoader struct {
	domain_config.ConfigLoader
}

type mockTracker struct {
	domain_pricing.CostTracker
}

type mockSessionLoader struct {
	domain_config.SessionLoader
}

type mockSkillSelector struct {
	skills.SkillSelector
}
