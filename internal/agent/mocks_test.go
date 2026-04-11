// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"sync"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/skills"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

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

// mockHistoryManager implements ports.HistoryManager for testing.
type mockHistoryManager struct {
	mu       sync.RWMutex
	Contents []*llm.Content
	Backup   []*llm.Content
	Resolver llm.AssetResolver

	AddContentFunc  func(ctx context.Context, content *llm.Content) error
	SetContentsFunc func(ctx context.Context, contents []*llm.Content) error
}

func (m *mockHistoryManager) GetTotalEntries() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.Contents)
}

func (m *mockHistoryManager) GetLastUserMessage(ctx context.Context) (string, int, error) {
	return "", 0, nil
}

func (m *mockHistoryManager) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := len(m.Contents)
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

	window := m.Contents[startIdx:endIdx]
	res := make([]*llm.Content, len(window))
	for i, c := range window {
		res[i] = llm.CloneContent(c)
	}
	return res, nil
}

func (m *mockHistoryManager) SetContents(ctx context.Context, contents []*llm.Content) error {
	if m.SetContentsFunc != nil {
		return m.SetContentsFunc(ctx, contents)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Contents = contents
	return nil
}

func (m *mockHistoryManager) Archive(ctx context.Context, contents []*llm.Content) error {
	return nil
}

func (m *mockHistoryManager) AddContent(ctx context.Context, content *llm.Content) error {
	if m.AddContentFunc != nil {
		return m.AddContentFunc(ctx, content)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Contents = append(m.Contents, llm.CloneContent(content))
	return nil
}

func (m *mockHistoryManager) GetResolver() llm.AssetResolver {
	return m.Resolver
}

func (m *mockHistoryManager) SetPinned(ctx context.Context, turnIndex int, pinned bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	startIdx := turnIndex * 2
	if startIdx < 0 || startIdx+1 >= len(m.Contents) {
		return fmt.Errorf("invalid turn index")
	}
	m.Contents[startIdx].Pinned = pinned
	m.Contents[startIdx+1].Pinned = pinned
	return nil
}

func (m *mockHistoryManager) Save(ctx context.Context) error {
	return nil
}

func (m *mockHistoryManager) RollbackTurns(ctx context.Context, turns int) (int, int, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	originalLen := len(m.Contents)
	if originalLen == 0 || turns <= 0 {
		return 0, originalLen / 2, originalLen, nil
	}

	removeMsgs := turns * 2
	var actualRemoved int
	if removeMsgs >= originalLen {
		actualRemoved = originalLen / 2
		m.Contents = nil
	} else {
		actualRemoved = turns
		m.Contents = m.Contents[:originalLen-removeMsgs]
	}

	remainingMsgs := len(m.Contents)
	remainingTurns := remainingMsgs / 2

	return actualRemoved, remainingTurns, remainingMsgs, nil
}

func (m *mockHistoryManager) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index >= 0 && index < len(m.Contents) {
		m.Contents[index].Parts = append(m.Contents[index].Parts, parts...)
	}
	return nil
}

func (m *mockHistoryManager) GetFilePath() string { return "" }

type mockExecutor struct {
	ExecuteFunc func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error)
}

func (m *mockExecutor) Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
	return m.ExecuteFunc(ctx, respContent, turn, maxToolTurns)
}

type mockEngineCostTracker struct {
	mu               sync.Mutex
	accumulatedCount int
}

func (m *mockEngineCostTracker) CalculateCost(mt llm.Metrics) float64 {
	return 0.05
}

func (m *mockEngineCostTracker) AccumulateAndReturn(mt llm.Metrics) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accumulatedCount++
	return 0.05
}

func (m *mockEngineCostTracker) Accumulate(mt llm.Metrics) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accumulatedCount++
}

func (m *mockEngineCostTracker) GetTotalCost(ctx context.Context) float64 {
	return 0
}

func (m *mockEngineCostTracker) GetDailyCost(ctx context.Context) float64 {
	return 0
}

func (m *mockEngineCostTracker) GetStats(ctx context.Context) (domain_pricing.UsageStats, float64) {
	return domain_pricing.UsageStats{}, 0
}

func (m *mockEngineCostTracker) Warmup() {}

func (m *mockHistoryManager) Sync(ctx context.Context) error {
	return nil
}
