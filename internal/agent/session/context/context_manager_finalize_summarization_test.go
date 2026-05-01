// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

type errorMockHistoryManager struct {
	agenttest.MockHistoryManager
	archiveErr        error
	setContentsErr    error
	archiveCalled     bool
	setContentsCalled bool
}

func (m *errorMockHistoryManager) Archive(ctx context.Context, contents []*llm.Content) error {
	m.archiveCalled = true
	return m.archiveErr
}

func (m *errorMockHistoryManager) SetContents(ctx context.Context, contents []*llm.Content) error {
	m.setContentsCalled = true
	return m.setContentsErr
}

// runFinalizeSummarizationErrorTest is a helper to reduce cyclomatic complexity by unifying repetitive test logic.
func runFinalizeSummarizationErrorTest(t *testing.T, archiveErr, setContentsErr error, expectedErr error, checkSetContents bool) {
	ctx := context.Background()
	content := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "hi"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "how are you?"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "I'm fine"}}},
	}

	h := &errorMockHistoryManager{}
	h.Contents = cloneContentSlice(content)
	h.archiveErr = archiveErr
	h.setContentsErr = setContentsErr

	cm := NewManager(NewContextStrategy(&agenttest.MockTokenCounter{}), h, nil, nil)
	cm.Summarizer = &agenttest.MockSummarizer{}

	_, _, err := cm.SummarizeRange(ctx, 1, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
	if !h.archiveCalled {
		t.Error("expected Archive to be called")
	}
	if checkSetContents {
		if !h.setContentsCalled {
			t.Error("expected SetContents to be called")
		}
	} else {
		if h.setContentsCalled {
			t.Error("expected SetContents NOT to be called")
		}
	}
}

func TestFinalizeSummarization_Errors(t *testing.T) {
	tests := []struct {
		name             string
		archiveErr       error
		setContentsErr   error
		expectedErr      error
		checkSetContents bool
	}{
		{"ArchiveTerminalError", errors.New("terminal disk error"), nil, llm.ErrTerminal, false},
		{"ArchiveTransientError", fmt.Errorf("%w: transient disk error", llm.ErrTransient), nil, llm.ErrTransient, false},
		{"SetContentsTerminalError", nil, errors.New("terminal disk error"), llm.ErrTerminal, true},
		{"SetContentsTransientError", nil, fmt.Errorf("%w: transient disk error", llm.ErrTransient), llm.ErrTransient, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runFinalizeSummarizationErrorTest(t, tt.archiveErr, tt.setContentsErr, tt.expectedErr, tt.checkSetContents)
		})
	}
}

// callCountingHM extends MockHistoryManager with a GetWindow that can be
// scripted to fail on a specific call index. All other methods delegate
// to the embedded MockHistoryManager.
type callCountingHM struct {
	agenttest.MockHistoryManager
	getWindowCallCount int
	failOnCallN        int   // 1-indexed: fail on the Nth GetWindow call
	getWindowErr       error // error to return on the failing call
}

func (m *callCountingHM) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	m.getWindowCallCount++
	if m.failOnCallN > 0 && m.getWindowCallCount == m.failOnCallN {
		return nil, m.getWindowErr
	}
	return m.MockHistoryManager.GetWindow(ctx, startIdx, endIdx)
}

// TestSummarizeRange_GetWindowErrorInCheckWindowSize verifies that a GetWindow
// error in checkWindowSize (the first call site, during boundary search)
// propagates back through SummarizeRange.
func TestSummarizeRange_GetWindowErrorInCheckWindowSize(t *testing.T) {
	ctx := context.Background()
	content := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "hi"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "how are you?"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "I'm fine"}}},
	}

	h := &callCountingHM{}
	h.Contents = cloneContentSlice(content)
	h.failOnCallN = 1
	h.getWindowErr = errors.New("db read error")

	cm := NewManager(NewContextStrategy(&agenttest.MockTokenCounter{}), h, nil, nil)
	cm.Summarizer = &agenttest.MockSummarizer{}

	_, _, err := cm.SummarizeRange(ctx, 1, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "db read error") {
		t.Errorf("expected error containing 'db read error', got: %v", err)
	}
}

// TestSummarizeRange_GetWindowErrorInFinalizeSummarization verifies that a
// GetWindow error in finalizeSummarization (the second call site, after the
// summarizer succeeds) propagates back through SummarizeRange.
func TestSummarizeRange_GetWindowErrorInFinalizeSummarization(t *testing.T) {
	ctx := context.Background()
	content := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "hi"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "how are you?"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "I'm fine"}}},
	}

	h := &callCountingHM{}
	h.Contents = cloneContentSlice(content)
	h.failOnCallN = 2
	h.getWindowErr = errors.New("concurrent modification")

	cm := NewManager(NewContextStrategy(&agenttest.MockTokenCounter{}), h, nil, nil)
	cm.Summarizer = &agenttest.MockSummarizer{}

	_, _, err := cm.SummarizeRange(ctx, 1, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "concurrent modification") {
		t.Errorf("expected error containing 'concurrent modification', got: %v", err)
	}
}
