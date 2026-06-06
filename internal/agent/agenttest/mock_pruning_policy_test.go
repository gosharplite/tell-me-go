// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestMockPruningPolicy_MarkTurns_Default(t *testing.T) {
	t.Parallel()

	m := &MockPruningPolicy{}
	n, err := m.MarkTurns(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("got n %d; want 0", n)
	}
}

func TestMockPruningPolicy_MarkTurns_Override(t *testing.T) {
	t.Parallel()

	wantN := 5
	wantErr := errors.New("pruning error")
	m := &MockPruningPolicy{
		MarkTurnsFn: func(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error) {
			return wantN, wantErr
		},
	}

	n, err := m.MarkTurns(context.Background(), nil, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v; want %v", err, wantErr)
	}
	if n != wantN {
		t.Errorf("got n %d; want %d", n, wantN)
	}
}

func TestMockPruningPolicy_Name_Default(t *testing.T) {
	t.Parallel()

	m := &MockPruningPolicy{}
	if got := m.Name(); got != "MockPolicy" {
		t.Errorf("got name %q; want %q", got, "MockPolicy")
	}
}

func TestMockPruningPolicy_Name_Override(t *testing.T) {
	t.Parallel()

	m := &MockPruningPolicy{
		NameFn: func() string { return "CustomPolicy" },
	}
	if got := m.Name(); got != "CustomPolicy" {
		t.Errorf("got name %q; want %q", got, "CustomPolicy")
	}
}
