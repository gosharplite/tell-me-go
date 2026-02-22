// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

type mockHistoryManager struct{}

func (m *mockHistoryManager) GetContents() []*llm.Content { return nil }
func (m *mockHistoryManager) SetContents(ctx context.Context, contents []*llm.Content) error {
	return nil
}
func (m *mockHistoryManager) Archive(ctx context.Context, contents []*llm.Content) error {
	return nil
}
func (m *mockHistoryManager) AddContent(ctx context.Context, content *llm.Content) error { return nil }
func (m *mockHistoryManager) GetResolver() llm.AssetResolver                             { return nil }
func (m *mockHistoryManager) SetPinned(ctx context.Context, turnIndex int, pinned bool) error {
	return nil
}
func (m *mockHistoryManager) Save(ctx context.Context) error { return nil }

func TestNewSession(t *testing.T) {
	id := "test-session-id"
	h := &mockHistoryManager{}

	before := time.Now().Add(-time.Second)
	session := NewSession(id, h)
	after := time.Now().Add(time.Second)

	if session.ID != id {
		t.Errorf("expected ID %s, got %s", id, session.ID)
	}
	if session.History != h {
		t.Error("expected history manager to match")
	}
	if session.StartTime.Before(before) || session.StartTime.After(after) {
		t.Errorf("StartTime %v is outside expected range [%v, %v]", session.StartTime, before, after)
	}
}

func (m *mockHistoryManager) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	return nil
}
