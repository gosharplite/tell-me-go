// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

type MockSecurityManager struct {
	domain_security.Manager
	AllowAll bool
}

func (m *MockSecurityManager) IsPathSafe(path string) (string, error) { return path, nil }
func (m *MockSecurityManager) TerminalLock()                          {}
func (m *MockSecurityManager) TerminalUnlock()                        {}
func (m *MockSecurityManager) IsCommandAllowed(command string) bool {
	return m.AllowAll
}

func (m *MockSecurityManager) Close() error { return nil }

// MockHistoryManager implements ports.HistoryManager for testing.
type MockHistoryManager struct {
	mu       sync.RWMutex
	Contents []*llm.Content
	Backup   []*llm.Content
	Resolver llm.AssetResolver

	AddContentFunc  func(ctx context.Context, content *llm.Content) error
	SetContentsFunc func(ctx context.Context, contents []*llm.Content) error
}

func (m *MockHistoryManager) GetTotalEntries() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.Contents)
}

func (m *MockHistoryManager) GetLastUserMessage(ctx context.Context) (string, int, error) {
	return "", 0, nil
}

func (m *MockHistoryManager) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
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

func (m *MockHistoryManager) SetContents(ctx context.Context, contents []*llm.Content) error {
	if m.SetContentsFunc != nil {
		return m.SetContentsFunc(ctx, contents)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Contents = contents
	return nil
}

func (m *MockHistoryManager) Archive(ctx context.Context, contents []*llm.Content) error {
	return nil
}

func (m *MockHistoryManager) AddContent(ctx context.Context, content *llm.Content) error {
	if m.AddContentFunc != nil {
		return m.AddContentFunc(ctx, content)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Contents = append(m.Contents, llm.CloneContent(content))
	return nil
}

func (m *MockHistoryManager) GetResolver() llm.AssetResolver {
	return m.Resolver
}

func (m *MockHistoryManager) SetPinned(ctx context.Context, turnIndex int, pinned bool) error {
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

func (m *MockHistoryManager) Save(ctx context.Context) error {
	return nil
}

func (m *MockHistoryManager) RollbackTurns(ctx context.Context, turns int) (int, int, int, error) {
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

func (m *MockHistoryManager) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index >= 0 && index < len(m.Contents) {
		m.Contents[index].Parts = append(m.Contents[index].Parts, parts...)
	}
	return nil
}

func (m *MockHistoryManager) GetFilePath() string { return "" }

func (m *MockHistoryManager) Sync(ctx context.Context) error {
	return nil
}
