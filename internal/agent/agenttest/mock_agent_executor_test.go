// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestMockAgentExecutor_Execute_DefaultPath(t *testing.T) {
	t.Parallel()

	m := &MockAgentExecutor{}
	content, err := m.Execute(context.Background(), nil, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != nil {
		t.Errorf("got content %+v; want nil", content)
	}
}

func TestMockAgentExecutor_Execute_WithFuncOverride(t *testing.T) {
	t.Parallel()

	wantContent := &llm.Content{Role: "assistant", Parts: []*llm.Part{{Text: "distinctive"}}}
	var gotRespContent *llm.Content
	var gotTurn int
	var gotMaxToolTurns int

	m := &MockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
			gotRespContent = respContent
			gotTurn = turn
			gotMaxToolTurns = maxToolTurns
			return wantContent, nil
		},
	}

	inputContent := &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "input"}}}
	content, err := m.Execute(context.Background(), inputContent, 3, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != wantContent {
		t.Errorf("got content %+v; want %+v", content, wantContent)
	}
	if gotRespContent != inputContent {
		t.Errorf("got respContent %+v; want %+v", gotRespContent, inputContent)
	}
	if gotTurn != 3 {
		t.Errorf("got turn %d; want 3", gotTurn)
	}
	if gotMaxToolTurns != 5 {
		t.Errorf("got maxToolTurns %d; want 5", gotMaxToolTurns)
	}
}
