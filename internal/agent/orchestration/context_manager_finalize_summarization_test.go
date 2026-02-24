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

func TestContextManager_FinalizeSummarization_Errors(t *testing.T) {
	ctx := context.Background()
	content := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "hi"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "how are you?"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "I'm fine"}}},
	}

	t.Run("Archive_TerminalError", func(t *testing.T) {
		h := &errorMockHistoryManager{}
		h.contents = cloneContentSlice(content)
		h.archiveErr = errors.New("terminal disk error")

		cm := NewContextManager(NewContextStrategy(&mockTokenCounter{}, nil), h, nil, nil)
		cm.Summarizer = &mockSummarizer{}

		_, _, err := cm.SummarizeRange(ctx, 1, "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, llm.ErrTerminal) {
			t.Errorf("expected llm.ErrTerminal, got %v", err)
		}
		if !h.archiveCalled {
			t.Error("expected Archive to be called")
		}
	})

	t.Run("Archive_TransientError", func(t *testing.T) {
		h := &errorMockHistoryManager{}
		h.contents = cloneContentSlice(content)
		h.archiveErr = fmt.Errorf("%w: transient disk error", llm.ErrTransient)

		cm := NewContextManager(NewContextStrategy(&mockTokenCounter{}, nil), h, nil, nil)
		cm.Summarizer = &mockSummarizer{}

		_, _, err := cm.SummarizeRange(ctx, 1, "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, llm.ErrTransient) {
			t.Errorf("expected llm.ErrTransient, got %v", err)
		}
		if !h.archiveCalled {
			t.Error("expected Archive to be called")
		}
	})

	t.Run("SetContents_TerminalError", func(t *testing.T) {
		h := &errorMockHistoryManager{}
		h.contents = cloneContentSlice(content)
		h.setContentsErr = errors.New("terminal disk error")

		cm := NewContextManager(NewContextStrategy(&mockTokenCounter{}, nil), h, nil, nil)
		cm.Summarizer = &mockSummarizer{}

		_, _, err := cm.SummarizeRange(ctx, 1, "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, llm.ErrTerminal) {
			t.Errorf("expected llm.ErrTerminal, got %v", err)
		}
		if !h.archiveCalled {
			t.Error("expected Archive to be called")
		}
		if !h.setContentsCalled {
			t.Error("expected SetContents to be called")
		}
	})

	t.Run("SetContents_TransientError", func(t *testing.T) {
		h := &errorMockHistoryManager{}
		h.contents = cloneContentSlice(content)
		h.setContentsErr = fmt.Errorf("%w: transient disk error", llm.ErrTransient)

		cm := NewContextManager(NewContextStrategy(&mockTokenCounter{}, nil), h, nil, nil)
		cm.Summarizer = &mockSummarizer{}

		_, _, err := cm.SummarizeRange(ctx, 1, "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, llm.ErrTransient) {
			t.Errorf("expected llm.ErrTransient, got %v", err)
		}
		if !h.archiveCalled {
			t.Error("expected Archive to be called")
		}
		if !h.setContentsCalled {
			t.Error("expected SetContents to be called")
		}
	})
}
