// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// MockHistoryManager is a test double for the history manager port. It
// stores LLM contents in memory under an exported RWMutex so that tests
// can inspect or mutate state directly. Behaviour for AddContent and
// SetContents may be overridden via the corresponding *Func fields;
// otherwise the mock applies the default in-memory semantics.
//
// MockHistoryManager satisfies ports.HistoryManager.
type MockHistoryManager struct {
	Mu             sync.RWMutex
	Contents       []*llm.Content
	resolver       llm.AssetResolver
	SetContentsErr error
	GetWindowErr   error
	RollbackErr    error

	AddContentFunc  func(ctx context.Context, content *llm.Content) error
	SetContentsFunc func(ctx context.Context, contents []*llm.Content) error
}

func (m *MockHistoryManager) GetTotalEntries() int {
	m.Mu.RLock()
	defer m.Mu.RUnlock()
	return len(m.Contents)
}

func (m *MockHistoryManager) GetLastUserMessage(ctx context.Context) (string, int, error) {
	return "", 0, nil
}

func (m *MockHistoryManager) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	if m.GetWindowErr != nil {
		return nil, m.GetWindowErr
	}
	m.Mu.RLock()
	defer m.Mu.RUnlock()

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
	cloned := make([]*llm.Content, len(window))
	for i, c := range window {
		cloned[i] = llm.CloneContent(c)
	}
	return cloned, nil
}

func (m *MockHistoryManager) SetContents(ctx context.Context, contents []*llm.Content) error {
	if m.SetContentsFunc != nil {
		return m.SetContentsFunc(ctx, contents)
	}
	if m.SetContentsErr != nil {
		return m.SetContentsErr
	}
	m.Mu.Lock()
	defer m.Mu.Unlock()
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
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.Contents = append(m.Contents, llm.CloneContent(content))
	return nil
}

func (m *MockHistoryManager) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	if index >= 0 && index < len(m.Contents) {
		m.Contents[index].Parts = append(m.Contents[index].Parts, parts...)
	}
	return nil
}

func (m *MockHistoryManager) GetResolver() llm.AssetResolver { return m.resolver }
func (m *MockHistoryManager) SetPinned(ctx context.Context, turnIndex int, pinned bool) error {
	return nil
}
func (m *MockHistoryManager) Save(ctx context.Context) error { return nil }
func (m *MockHistoryManager) Sync(ctx context.Context) error { return nil }
func (m *MockHistoryManager) RollbackTurns(ctx context.Context, turns int) (int, int, int, error) {
	if m.RollbackErr != nil {
		return 0, 0, 0, m.RollbackErr
	}
	m.Mu.Lock()
	defer m.Mu.Unlock()
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
	return actualRemoved, len(m.Contents) / 2, len(m.Contents), nil
}
func (m *MockHistoryManager) GetFilePath() string       { return "" }
func (m *MockHistoryManager) SetGetWindowErr(err error) { m.GetWindowErr = err }
func (m *MockHistoryManager) SetInternalContents(contents []*llm.Content) {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.Contents = contents
}

func (m *MockHistoryManager) GetContents() []*llm.Content {
	m.Mu.RLock()
	defer m.Mu.RUnlock()
	res := make([]*llm.Content, len(m.Contents))
	for i, c := range m.Contents {
		res[i] = llm.CloneContent(c)
	}
	return res
}

func (m *MockHistoryManager) SetSetContentsErr(err error) {
	m.SetContentsErr = err
}

func (m *MockHistoryManager) SetRollbackErr(err error) {
	m.RollbackErr = err
}
