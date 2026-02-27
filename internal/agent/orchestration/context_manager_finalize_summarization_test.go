// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

type errorMockHistoryManager struct {
	mockHistoryManager
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
	h.contents = cloneContentSlice(content)
	h.archiveErr = archiveErr
	h.setContentsErr = setContentsErr

	cm := NewContextManager(NewContextStrategy(&mockTokenCounter{}, nil), h, nil, nil)
	cm.Summarizer = &mockSummarizer{}

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
